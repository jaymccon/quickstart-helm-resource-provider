package resource

import (
	"context"
	"encoding/base64"
	"log"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

type clusterData struct {
	endpoint string
	CAData   []byte
}

// getClusterDetails use describe_cluster API
func getClusterDetails(session *session.Session, clusterName string) (*clusterData, error) {
	log.Printf("Getting cluster data...")
	c := &clusterData{}
	svc := eks.New(session)
	input := &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	}
	result, err := svc.DescribeCluster(input)
	if err != nil {
		return nil, AWSError(err)
	}
	c.endpoint = *result.Cluster.Endpoint
	c.CAData, err = base64.StdEncoding.DecodeString(*result.Cluster.CertificateAuthority.Data)
	if err != nil {
		return nil, genericError("Decoding CA", err)
	}
	return c, nil
}

// generateKubeToken using the aws-iam-auth pkg
func generateKubeToken(session *session.Session, clusterID string) (*string, error) {
	log.Printf("Generating token for cluster %s", clusterID)
	gen, err := token.NewGenerator(false, false)
	if err != nil {
		return nil, genericError("Could not get token: ", err)
	}
	tok, err := gen.GetWithOptions(&token.GetTokenOptions{
		ClusterID: clusterID,
		Session:   session,
	})
	if err != nil {
		return nil, genericError("Could not get token: ", err)
	}
	return &tok.Token, nil
}

// downloadS3 download file from S3 to specified path.
func downloadS3(session *session.Session, bucket string, key string, filename string) error {
	log.Printf("Getting file from S3...")
	region, err := getBucketRegion(session, bucket)
	if err != nil {
		return err
	}
	sess := session.Copy(&aws.Config{Region: region})
	// Create a downloader with the session and default options
	downloader := s3manager.NewDownloader(sess)

	// Create a file to write the S3 Object contents to.
	f, err := os.Create(filename)
	if err != nil {
		return genericError("downloadS3", err)
	}

	// Write the contents of S3 Object to the file
	numBytes, err := downloader.Download(f, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return AWSError(err)
	}
	log.Printf("Downloaded %s - %v bytes ", f.Name(), numBytes)
	return nil
}

//getSecretsManager and returns bytes data.
func getSecretsManager(session *session.Session, arn *string) ([]byte, error) {
	log.Printf("Getting data from Secrets Manager...")
	svc := secretsmanager.New(session)
	input := &secretsmanager.GetSecretValueInput{
		SecretId:     arn,
		VersionStage: aws.String("AWSCURRENT"),
	}
	result, err := svc.GetSecretValue(input)
	if err != nil {
		return nil, AWSError(err)
	}

	// Decrypts secret using the associated KMS CMK.
	// Depending on whether the secret is a string or binary, one of these fields will be populated.
	var secretString []byte
	if result.SecretString != nil {
		secretString = []byte(*result.SecretString)
	} else {
		decodedBinarySecretBytes := make([]byte, base64.StdEncoding.DecodedLen(len(result.SecretBinary)))
		len, err := base64.StdEncoding.Decode(decodedBinarySecretBytes, result.SecretBinary)
		if err != nil {
			return nil, genericError("Base64 Decode Error:", err)
		}
		secretString = []byte(string(decodedBinarySecretBytes[:len]))
	}
	return secretString, nil
}

func getBucketRegion(session *session.Session, bucket string) (*string, error) {
	log.Printf("Checking S3 bucket region...")
	ctx := context.Background()
	region, err := s3manager.GetBucketRegion(ctx, session, bucket, *session.Config.Region)
	if err != nil {
		return nil, AWSError(err)
	}
	log.Printf("Found S3 bucket region: %v", region)
	return aws.String(region), nil
}
