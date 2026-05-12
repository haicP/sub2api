-- Migration: 137_add_request_detail_response_content
-- 为请求详情增加单独的响应内容字段，保存最终用户可见文本回复。

ALTER TABLE request_details
    ADD COLUMN IF NOT EXISTS response_content TEXT NOT NULL DEFAULT '';
