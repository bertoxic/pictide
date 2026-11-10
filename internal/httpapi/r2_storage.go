package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"pictide.local/studio/internal/config"
)

const maxPhotoSize = 30 << 20

type r2Storage struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
}

func newR2Storage(c config.Config) (*r2Storage, error) {
	if c.R2AccountID == "" && c.R2AccessKeyID == "" && c.R2SecretAccessKey == "" && c.R2BucketName == "" {
		return nil, nil
	}
	if c.R2AccountID == "" || c.R2AccessKeyID == "" || c.R2SecretAccessKey == "" || c.R2BucketName == "" {
		return nil, errors.New("all R2 credentials and bucket name are required")
	}
	for _, r := range c.R2AccountID {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return nil, errors.New("invalid R2 account ID")
		}
	}
	client := s3.New(s3.Options{Region: "auto", BaseEndpoint: aws.String("https://" + c.R2AccountID + ".r2.cloudflarestorage.com"), Credentials: credentials.NewStaticCredentialsProvider(c.R2AccessKeyID, c.R2SecretAccessKey, ""), UsePathStyle: true, RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired, ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired})
	return &r2Storage{client: client, presigner: s3.NewPresignClient(client), bucket: c.R2BucketName}, nil
}

func (s *r2Storage) putURL(ctx context.Context, key string) (string, error) {
	req, err := s.presigner.PresignPutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), ContentType: aws.String("application/octet-stream")}, s3.WithPresignExpires(10*time.Minute))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}
func (s *r2Storage) getURL(ctx context.Context, key string) (string, error) {
	req, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}, s3.WithPresignExpires(2*time.Minute))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}
func (s *r2Storage) put(ctx context.Context, key, contentType string, data []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), ContentType: aws.String(contentType), Body: bytes.NewReader(data)})
	return err
}
func (s *r2Storage) get(ctx context.Context, key string, limit int64) ([]byte, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()
	if result.ContentLength != nil && *result.ContentLength > limit {
		return nil, errors.New("object too large")
	}
	data, err := io.ReadAll(io.LimitReader(result.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("object too large")
	}
	return data, nil
}
func (s *r2Storage) delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}
func (s *r2Storage) open(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}
func (s *r2Storage) putArchive(ctx context.Context, key string, write func(io.Writer) error) error {
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { err := write(writer); _ = writer.CloseWithError(err); done <- err }()
	uploader := manager.NewUploader(s.client, func(u *manager.Uploader) { u.Concurrency = 1; u.PartSize = 5 << 20 })
	_, uploadErr := uploader.Upload(ctx, &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), ContentType: aws.String("application/zip"), Body: reader})
	if uploadErr != nil {
		_ = reader.CloseWithError(uploadErr)
	} else {
		_ = reader.Close()
	}
	writeErr := <-done
	if uploadErr != nil {
		return uploadErr
	}
	return writeErr
}
func r2PhotoKey(studio, id, variant string) string {
	return "pictide/studios/" + url.PathEscape(studio) + "/photos/" + url.PathEscape(id) + "/" + variant
}
func r2TemporaryKey(studio, id string) string {
	return "pictide/temporary/uploads/" + url.PathEscape(studio) + "/" + url.PathEscape(id)
}
