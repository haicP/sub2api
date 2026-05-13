package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RequestDetailImageArtifact records image artifact metadata for request details.
type RequestDetailImageArtifact struct {
	ent.Schema
}

func (RequestDetailImageArtifact) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "request_detail_image_artifacts"},
	}
}

func (RequestDetailImageArtifact) Fields() []ent.Field {
	return []ent.Field{
		field.String("request_id").
			MaxLen(64).
			NotEmpty(),
		field.String("direction").
			MaxLen(16).
			Default(""),
		field.String("source").
			MaxLen(64).
			Default(""),
		field.String("status").
			MaxLen(16).
			Default(""),
		field.String("s3_key").
			Default("").
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("original_url").
			Default("").
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("content_type").
			MaxLen(128).
			Default(""),
		field.String("file_name").
			MaxLen(255).
			Default(""),
		field.Int64("size_bytes").
			Default(0),
		field.String("sha256").
			MaxLen(64).
			Default(""),
		field.Int("image_index").
			Optional().
			Nillable(),
		field.JSON("metadata", map[string]any{}).
			Default(map[string]any{}).
			Annotations(entsql.DefaultExprs(map[string]string{
				dialect.Postgres: "'{}'::jsonb",
				dialect.SQLite:   "'{}'",
			})),
		field.String("error_message").
			Default("").
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("created_at").
			Default(time.Now).
			Immutable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (RequestDetailImageArtifact) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("request_id", "id").
			StorageKey("idx_request_detail_image_artifacts_request_id"),
		index.Fields("created_at").
			StorageKey("idx_request_detail_image_artifacts_created_at"),
	}
}
