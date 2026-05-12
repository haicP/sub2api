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

// RequestDetail 记录单次请求的完整调试明细。
type RequestDetail struct {
	ent.Schema
}

func (RequestDetail) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "request_details"},
	}
}

func (RequestDetail) Fields() []ent.Field {
	return []ent.Field{
		field.String("request_id").
			MaxLen(64).
			NotEmpty().
			Unique(),
		field.Time("created_at").
			Default(time.Now).
			Immutable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("completed_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int("duration_ms").
			Optional().
			Nillable(),
		field.Int("status_code").
			Default(0),
		field.Bool("success").
			Default(false),
		field.String("platform").
			MaxLen(32).
			Default(""),
		field.String("endpoint").
			MaxLen(255).
			Default(""),
		field.String("upstream_endpoint").
			MaxLen(1024).
			Default(""),
		field.String("model").
			MaxLen(255).
			Default(""),
		field.String("upstream_model").
			MaxLen(255).
			Default(""),
		field.Bool("stream").
			Default(false),
		field.Int64("user_id").
			Optional().
			Nillable(),
		field.Int64("api_key_id").
			Optional().
			Nillable(),
		field.Int64("account_id").
			Optional().
			Nillable(),
		field.Int64("group_id").
			Optional().
			Nillable(),
		field.Int64("subscription_id").
			Optional().
			Nillable(),
		field.String("ip_address").
			MaxLen(45).
			Default(""),
		field.String("user_agent").
			MaxLen(512).
			Default(""),
		field.JSON("request_headers", map[string][]string{}).
			Default(map[string][]string{}).
			Annotations(entsql.DefaultExprs(map[string]string{
				dialect.Postgres: "'{}'::jsonb",
				dialect.SQLite:   "'{}'",
			})),
		field.JSON("response_headers", map[string][]string{}).
			Default(map[string][]string{}).
			Annotations(entsql.DefaultExprs(map[string]string{
				dialect.Postgres: "'{}'::jsonb",
				dialect.SQLite:   "'{}'",
			})),
		field.String("request_body").
			Default("").
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("upstream_request_body").
			Default("").
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("response_body").
			Default("").
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("error_message").
			Default("").
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Bool("response_truncated").
			Default(false),
	}
}

func (RequestDetail) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("created_at").
			StorageKey("idx_request_details_created_at"),
		index.Fields("user_id", "created_at").
			StorageKey("idx_request_details_user_created_at"),
		index.Fields("api_key_id", "created_at").
			StorageKey("idx_request_details_api_key_created_at"),
		index.Fields("account_id", "created_at").
			StorageKey("idx_request_details_account_created_at"),
		index.Fields("model", "created_at").
			StorageKey("idx_request_details_model_created_at"),
		index.Fields("platform", "created_at").
			StorageKey("idx_request_details_platform_created_at"),
		index.Fields("status_code", "created_at").
			StorageKey("idx_request_details_status_created_at"),
	}
}
