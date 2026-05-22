package repository

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// S3BackupStore implements service.BackupObjectStore using AWS S3 compatible storage
type S3BackupStore struct {
	client *s3.Client
	bucket string
}

// NewS3BackupStoreFactory returns a BackupObjectStoreFactory that creates S3-backed stores
func NewS3BackupStoreFactory() service.BackupObjectStoreFactory {
	return func(ctx context.Context, cfg *service.BackupS3Config) (service.BackupObjectStore, error) {
		region := cfg.Region
		if region == "" {
			region = "auto" // Cloudflare R2 默认 region
		}

		awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
			awsconfig.WithRegion(region),
			awsconfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
			),
		)
		if err != nil {
			return nil, fmt.Errorf("load aws config: %w", err)
		}

		client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			if cfg.Endpoint != "" {
				o.BaseEndpoint = &cfg.Endpoint
			}
			if cfg.ForcePathStyle {
				o.UsePathStyle = true
			}
			o.APIOptions = append(o.APIOptions, v4.SwapComputePayloadSHA256ForUnsignedPayloadMiddleware)
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		})

		return &S3BackupStore{client: client, bucket: cfg.Bucket}, nil
	}
}

func (s *S3BackupStore) Upload(ctx context.Context, key string, body io.Reader, contentType string) (int64, error) {
	tmp, sizeBytes, err := spoolBackupUploadBody(body)
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	defer func() { _ = tmp.Close() }()

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        &s.bucket,
		Key:           &key,
		Body:          tmp,
		ContentLength: aws.Int64(sizeBytes),
		ContentType:   &contentType,
	})
	if err != nil {
		return 0, fmt.Errorf("S3 PutObject: %w", err)
	}
	info, err := s.HeadObject(ctx, key)
	if err != nil {
		headErr := err
		if !isS3Forbidden(headErr) {
			return 0, fmt.Errorf("S3 HeadObject after PutObject: %w", headErr)
		}
		info, err = s.confirmObjectWithList(ctx, key)
		if err != nil {
			listErr := err
			info, err = s.confirmObjectWithRangeGet(ctx, key)
			if err != nil {
				return 0, fmt.Errorf("S3 HeadObject forbidden and object confirmation failed after PutObject: head=%v list=%v get=%w", headErr, listErr, err)
			}
		}
	}
	if info.SizeBytes > 0 && info.SizeBytes != sizeBytes {
		return 0, fmt.Errorf("S3 HeadObject size mismatch after PutObject: uploaded=%d stored=%d key=%s", sizeBytes, info.SizeBytes, key)
	}
	return sizeBytes, nil
}

func spoolBackupUploadBody(body io.Reader) (*os.File, int64, error) {
	// PutObject needs a replayable body with a known size for broad S3-compatible
	// storage support. Spool to disk so large backups do not have to fit in RAM.
	tmp, err := os.CreateTemp("", "sub2api-s3-upload-*")
	if err != nil {
		return nil, 0, fmt.Errorf("create temp upload file: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	sizeBytes, err := io.Copy(tmp, body)
	if err != nil {
		return nil, 0, fmt.Errorf("spool upload body: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, 0, fmt.Errorf("rewind temp upload file: %w", err)
	}
	cleanup = false
	return tmp, sizeBytes, nil
}

func (s *S3BackupStore) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, fmt.Errorf("S3 GetObject: %w", err)
	}
	return result.Body, nil
}

func (s *S3BackupStore) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	return err
}

func (s *S3BackupStore) PresignURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(s.client)
	result, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("presign url: %w", err)
	}
	return result.URL, nil
}

func (s *S3BackupStore) HeadBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: &s.bucket,
	})
	if err != nil {
		return fmt.Errorf("S3 HeadBucket failed: %w", err)
	}
	return nil
}

func (s *S3BackupStore) HeadObject(ctx context.Context, key string) (*service.BackupObjectInfo, error) {
	result, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, fmt.Errorf("S3 HeadObject failed: %w", err)
	}
	info := &service.BackupObjectInfo{}
	if result.ContentLength != nil {
		info.SizeBytes = *result.ContentLength
	}
	if result.ETag != nil {
		info.ETag = *result.ETag
	}
	if result.LastModified != nil {
		info.LastModified = *result.LastModified
	}
	return info, nil
}

func (s *S3BackupStore) confirmObjectWithRangeGet(ctx context.Context, key string) (*service.BackupObjectInfo, error) {
	byteRange := "bytes=0-0"
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
		Range:  &byteRange,
	})
	if err != nil {
		return nil, fmt.Errorf("S3 GetObject range confirmation failed: %w", err)
	}
	defer func() { _ = result.Body.Close() }()
	if _, err := io.Copy(io.Discard, result.Body); err != nil {
		return nil, fmt.Errorf("read S3 GetObject range confirmation: %w", err)
	}
	info := &service.BackupObjectInfo{}
	if result.ContentRange != nil {
		if sizeBytes, ok := parseS3ContentRangeSize(*result.ContentRange); ok {
			info.SizeBytes = sizeBytes
		}
	}
	if result.ETag != nil {
		info.ETag = *result.ETag
	}
	if result.LastModified != nil {
		info.LastModified = *result.LastModified
	}
	return info, nil
}

func (s *S3BackupStore) confirmObjectWithList(ctx context.Context, key string) (*service.BackupObjectInfo, error) {
	maxKeys := int32(1)
	result, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  &s.bucket,
		Prefix:  &key,
		MaxKeys: &maxKeys,
	})
	if err != nil {
		return nil, fmt.Errorf("S3 ListObjectsV2 confirmation failed: %w", err)
	}
	for _, object := range result.Contents {
		if object.Key == nil || *object.Key != key {
			continue
		}
		info := &service.BackupObjectInfo{}
		if object.Size != nil {
			info.SizeBytes = *object.Size
		}
		if object.ETag != nil {
			info.ETag = *object.ETag
		}
		if object.LastModified != nil {
			info.LastModified = *object.LastModified
		}
		return info, nil
	}
	return nil, fmt.Errorf("S3 ListObjectsV2 confirmation did not find key: %s", key)
}

func isS3Forbidden(err error) bool {
	var responseErr *smithyhttp.ResponseError
	if errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == http.StatusForbidden {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := strings.ToLower(strings.TrimSpace(apiErr.ErrorCode()))
		return code == "forbidden" || code == "accessdenied" || code == "accessdeniedexception"
	}
	return false
}

func parseS3ContentRangeSize(value string) (int64, bool) {
	_, suffix, ok := strings.Cut(strings.TrimSpace(value), "/")
	if !ok || suffix == "" || suffix == "*" {
		return 0, false
	}
	sizeBytes, err := strconv.ParseInt(suffix, 10, 64)
	if err != nil || sizeBytes < 0 {
		return 0, false
	}
	return sizeBytes, true
}
