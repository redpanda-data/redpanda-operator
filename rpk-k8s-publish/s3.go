// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithy "github.com/aws/smithy-go"
)

// s3Store is the production objectStore backed by an AWS S3 bucket.
type s3Store struct {
	client *s3.Client
	bucket string
}

func newS3Store(ctx context.Context, region, bucket string) (*s3Store, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS config: %w", err)
	}
	return &s3Store{client: s3.NewFromConfig(cfg), bucket: bucket}, nil
}

func (s *s3Store) put(ctx context.Context, key string, body []byte, tags map[string]string) error {
	tagging := url.Values{}
	for k, v := range tags {
		tagging.Set(k, v)
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(body),
		// max-age=1 minimizes manifest/archive inconsistency window behind
		// CloudFront, matching connect's uploader.
		CacheControl: aws.String("max-age=1"),
		Tagging:      aws.String(tagging.Encode()),
	})
	return err
}

func (s *s3Store) list(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	p := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}
	}
	return keys, nil
}

func (s *s3Store) head(ctx context.Context, key string) (map[string]string, bool, error) {
	out, err := s.client.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		// A missing object is not an error for our purposes — report it as
		// non-existent so callers can decide whether to upload. S3 returns
		// NoSuchKey (and sometimes NotFound) as a smithy API error code.
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.ErrorCode() {
			case "NoSuchKey", "NotFound":
				return nil, false, nil
			}
		}
		return nil, false, err
	}
	tags := map[string]string{}
	for _, t := range out.TagSet {
		tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return tags, true, nil
}
