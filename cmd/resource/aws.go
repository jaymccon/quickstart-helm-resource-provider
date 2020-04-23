package resource

import (
	"context"
	"encoding/base64"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/eks/eksiface"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/aws/aws-sdk-go/service/lambda/lambdaiface"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/aws/aws-sdk-go/service/secretsmanager/secretsmanageriface"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/aws/aws-sdk-go/service/sts/stsiface"
	"sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

type clusterData struct {
	endpoint string
	CAData   []byte
}

type S3API s3iface.S3API
type LambdaAPI lambdaiface.LambdaAPI
type STSAPI stsiface.STSAPI
type SecretsManagerAPI secretsmanageriface.SecretsManagerAPI
type EKSAPI eksiface.EKSAPI

type AWSClients interface {
	S3Client(region *string, role *string) S3API
	LambdaClient(region *string, role *string) LambdaAPI
	STSClient(region *string, role *string) STSAPI
	SecretsManagerClient(region *string, role *string) SecretsManagerAPI
	EKSClient(region *string, role *string) EKSAPI
}

var _ AWSClients = (*Clients)(nil)

func (c *Clients) S3Client(region *string, role *string) S3API {
	return s3.New(c.Session(region, role))
}

func (c *Clients) LambdaClient(region *string, role *string) LambdaAPI {
	return lambda.New(c.Session(region, role))
}

func (c *Clients) STSClient(region *string, role *string) STSAPI {
	return sts.New(c.Session(region, role))
}

func (c *Clients) SecretsManagerClient(region *string, role *string) SecretsManagerAPI {
	return secretsmanager.New(c.Session(region, role))
}
func (c *Clients) EKSClient(region *string, role *string) EKSAPI {
	return eks.New(c.Session(region, role))
}

func (c *Clients) Session(region *string, role *string) *session.Session {
	if region != nil {
		return c.AWSSession.Copy(c.Config(region, role))
	}
	return c.AWSSession
}

func (c *Clients) Config(region *string, role *string) *aws.Config {
	config := aws.NewConfig().WithMaxRetries(10)

	if region != nil {
		config = config.WithRegion(*region)
	}
	if role != nil {
		creds := stscreds.NewCredentials(c.AWSSession, *role)
		config = config.WithCredentials(creds)
	}
	return config
}

// getClusterDetails use describe_cluster API
func getClusterDetails(svc eksiface.EKSAPI, clusterName string) (*clusterData, error) {
	log.Printf("Getting cluster data...")
	c := &clusterData{}
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
func generateKubeToken(svc STSAPI, clusterID *string) (*string, error) {
	log.Printf("Generating token for cluster %s", *clusterID)
	gen, err := token.NewGenerator(false, false)
	if err != nil {
		return nil, genericError("Could not get token: ", err)
	}
	tok, err := gen.GetWithSTS(*clusterID, svc)
	if err != nil {
		return nil, genericError("Could not get token: ", err)
	}
	return &tok.Token, nil
}

// downloadS3 download file from S3 to specified path.
func downloadS3(svc S3API, bucket string, key string, filename string) error {
	log.Printf("Getting file from S3...")

	// Create a downloader with the session and default options
	downloader := s3manager.NewDownloaderWithClient(svc)

	// Create a file to write the S3 Object contents to.
	f, err := os.Create(filename)
	if err != nil {
		return genericError("downloadS3", err)
	}

	// Write the contents of S3 Object to the file

	// Write the contents of S3 Object to the file
	numBytes, err := downloader.Download(f, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return genericError("downloadS3", err)
	}

	log.Printf("Downloaded %s - %v bytes ", f.Name(), numBytes)
	return nil
}

//getSecretsManager and returns bytes data.
func getSecretsManager(svc SecretsManagerAPI, arn *string) ([]byte, error) {
	log.Printf("Getting data from Secrets Manager...")

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

func getBucketRegion(svc S3API, bucket string) (*string, error) {
	log.Printf("Checking S3 bucket region...")
	ctx := context.Background()
	region, err := s3manager.GetBucketRegionWithClient(ctx, svc, bucket)
	if err != nil {
		return nil, AWSError(err)
	}
	log.Printf("Found S3 bucket region: %v", region)
	return aws.String(region), nil
}

func getCurrentRoleARN(svc STSAPI) (*string, error) {
	input := &sts.GetCallerIdentityInput{}
	response, err := svc.GetCallerIdentity(input)
	if err != nil {
		return nil, AWSError(err)
	}
	return toRoleArn(response.Arn), nil
}

func toRoleArn(arn *string) *string {
	arnParts := strings.Split(*arn, ":")
	if arnParts[2] != "sts" || !strings.HasPrefix(arnParts[5], "assumed-role") {
		return arn
	}
	arnParts = strings.Split(*arn, "/")
	arnParts[0] = strings.Replace(arnParts[0], "assumed-role", "role", 1)
	arnParts[0] = strings.Replace(arnParts[0], ":sts:", ":iam:", 1)
	arn = aws.String(arnParts[0] + "/" + arnParts[1])
	return arn
}
