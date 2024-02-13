package storage

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	awsSession "github.com/aws/aws-sdk-go/aws/session"
	awsS3 "github.com/aws/aws-sdk-go/service/s3"
)

// Client is an interface to the S3-compatible bucket where we keep generated images for
// display and archival
type Client interface {
	Upload(ctx context.Context, key string, contentType string, data io.ReadSeeker) (string, error)
}

// client implements imagegen.StorageClient using the S3 API to connect to a
// DigitalOcean Spaces bucket (e.g. 'golden-vcr-user-images')
type client struct {
	s3         *awsS3.S3
	bucketName string
	baseUrl    string
}

// NewClient initializes a storage.Client that will allow generated image files to be
// uploaded to a Spaces bucket
func NewClient(spacesAccessKeyId, spacesSecretKey, spacesEndpointOrigin, spacesRegionName, spacesBucketName string) (Client, error) {
	config := &aws.Config{
		Credentials:      credentials.NewStaticCredentials(spacesAccessKeyId, spacesSecretKey, ""),
		Endpoint:         aws.String(fmt.Sprintf("https://%s", spacesEndpointOrigin)),
		Region:           aws.String(spacesRegionName),
		S3ForcePathStyle: aws.Bool(false),
	}
	session, err := awsSession.NewSession(config)
	if err != nil {
		return nil, err
	}
	s3 := awsS3.New(session)
	return &client{
		s3:         s3,
		bucketName: spacesBucketName,
		baseUrl:    fmt.Sprintf("https://%s.%s", spacesBucketName, spacesEndpointOrigin),
	}, nil
}

// Uploads stores a file in S3 and returns the URL at which a user can later access it
func (c *client) Upload(ctx context.Context, key string, contentType string, data io.ReadSeeker) (string, error) {
	_, err := c.s3.PutObjectWithContext(ctx, &awsS3.PutObjectInput{
		Bucket:      aws.String(c.bucketName),
		Key:         aws.String(key),
		Body:        data,
		ACL:         aws.String("public-read"),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s", c.baseUrl, key), nil
}
