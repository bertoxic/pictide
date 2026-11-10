package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/aws/smithy-go"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"pictide.local/studio/internal/config"
)

type r2Storage struct {
	client         *s3.Client
	bucket, prefix string
}

func newR2Storage(c config.Config) (*r2Storage, error) {
	prefix := c.R2Prefix
	if prefix == "" {
		prefix = "legacy"
	}
	client := s3.New(s3.Options{Region: "auto", BaseEndpoint: aws.String("https://" + c.R2AccountID + ".r2.cloudflarestorage.com"), Credentials: credentials.NewStaticCredentialsProvider(c.R2AccessKeyID, c.R2SecretAccessKey, ""), UsePathStyle: true, RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired, ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired})
	return &r2Storage{client: client, bucket: c.R2BucketName, prefix: prefix}, nil
}
func (s *Server) writeMedia(name string, data []byte) error {
	if s.r2 != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, err := s.r2.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.r2.bucket), Key: aws.String(s.r2.prefix + "/" + name), Body: bytes.NewReader(data)})
		if err != nil {
			return fmt.Errorf("R2 upload: %w", err)
		}
		return nil
	}
	return os.WriteFile(filepath.Join(s.Config.MediaDir, name), data, 0600)
}
func (s *Server) remoteMedia(name string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	obj, err := s.r2.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.r2.bucket), Key: aws.String(s.r2.prefix + "/" + name)})
	if err != nil {
		return nil, err
	}
	defer obj.Body.Close()
	return io.ReadAll(obj.Body)
}
func (s *Server) readMedia(name string) ([]byte, error) {
	if s.r2 != nil {
		return s.remoteMedia(name)
	}
	return os.ReadFile(filepath.Join(s.Config.MediaDir, name))
}

// Migrate only regular media files; never delete until the remote bytes have been verified.
func (s *Server) migrateMedia() error {
	if s.r2 == nil {
		return nil
	}
	entries, err := os.ReadDir(s.Config.MediaDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || len(entry.Name()) == 0 || entry.Name()[0] == '.' {
			continue
		}
		name := entry.Name()
		file := filepath.Join(s.Config.MediaDir, name)
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		remote, err := s.remoteMedia(name)
		if err != nil {
			var apiErr smithy.APIError
			if !errors.As(err, &apiErr) || (apiErr.ErrorCode() != "NoSuchKey" && apiErr.ErrorCode() != "NotFound") {
				return fmt.Errorf("check R2 media: %w", err)
			}
			if err = s.writeMedia(name, data); err != nil {
				return err
			}
			remote, err = s.remoteMedia(name)
			if err != nil {
				return err
			}
		}
		if !bytes.Equal(data, remote) {
			return fmt.Errorf("R2 media differs from local file %s; local file retained", name)
		}
		current, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, data) {
			return fmt.Errorf("local media changed during migration: %s", name)
		}
		if err = os.Remove(file); err != nil {
			return err
		}
	}
	return nil
}
func (s *Server) removeMedia(name string) error {
	if s.r2 != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := s.r2.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.r2.bucket), Key: aws.String(s.r2.prefix + "/" + name)})
		if err != nil {
			return err
		}
	}
	err := os.Remove(filepath.Join(s.Config.MediaDir, name))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Server) serveMedia(w http.ResponseWriter, r *http.Request, name string) {
	data, err := s.readMedia(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
