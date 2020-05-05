package resource

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client/metadata"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/stretchr/testify/assert"
)

// Define mock structs.
type mockEKSClient struct {
	EKSAPI
}

type mockSecretsManagerClient struct {
	SecretsManagerAPI
}

type mockSTSClient struct {
	STSAPI
}

type mockS3Client struct {
	S3API
}

func (m *mockAWSClients) EKSClient(region *string, role *string) EKSAPI {
	return &mockEKSClient{}
}
func (m *mockAWSClients) S3Client(region *string, role *string) S3API {
	return &mockS3Client{}
}
func (m *mockAWSClients) STSClient(region *string, role *string) STSAPI {
	return &mockSTSClient{}
}
func (m *mockAWSClients) LambdaClient(region *string, role *string) LambdaAPI {
	return &mockLambdaClient{}
}
func (m *mockAWSClients) SecretsManagerClient(region *string, role *string) SecretsManagerAPI {
	return &mockSecretsManagerClient{}
}
func (m *mockAWSClients) Session(region *string, role *string) *session.Session {
	return MockSession
}

func (m *mockEKSClient) DescribeCluster(c *eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error) {
	clusters := map[string]struct {
		data *eks.Cluster
	}{
		"eks": {
			data: &eks.Cluster{
				Arn: aws.String("arn:aws:eks:us-east-2:1234567890:cluster/eks"),
				CertificateAuthority: &eks.Certificate{
					Data: aws.String("LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCi0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0="),
				},
				Endpoint: aws.String("https://EKS.yl4.us-east-2.eks.amazonaws.com"),
				Name:     aws.String("eks"),
				Status:   aws.String(eks.ClusterStatusActive),
			},
		},
		"eks1": {
			data: &eks.Cluster{
				Arn:    aws.String("arn:aws:eks:us-east-2:1234567890:cluster/eks1"),
				Name:   aws.String("eks1"),
				Status: aws.String(eks.ClusterStatusCreating),
			},
		},
		"eks2": {
			data: &eks.Cluster{
				Arn: aws.String("arn:aws:eks:us-east-2:1234567890:cluster/eks2"),
				CertificateAuthority: &eks.Certificate{
					Data: aws.String("LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCi0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0="),
				},
				Endpoint: aws.String("https://EKS2.yl4.us-east-2.eks.amazonaws.com"),
				Name:     aws.String("eks2"),
				Status:   aws.String(eks.ClusterStatusUpdating),
			},
		},
	}
	for name, d := range clusters {
		if aws.StringValue(c.Name) == name {
			return &eks.DescribeClusterOutput{
				Cluster: d.data,
			}, nil
		}
	}
	return &eks.DescribeClusterOutput{
		Cluster: nil,
	}, fmt.Errorf("%s", eks.ErrCodeNotFoundException)
}

func (m *mockSecretsManagerClient) GetSecretValue(s *secretsmanager.GetSecretValueInput) (*secretsmanager.GetSecretValueOutput, error) {
	secrets := map[string]struct {
		GetSecretValueOutput *secretsmanager.GetSecretValueOutput
	}{
		"sec1": {
			GetSecretValueOutput: &secretsmanager.GetSecretValueOutput{
				ARN:          aws.String("arn:aws:secretsmanager:us-east-2:1234567890:secret:kubeconfig-Wt"),
				Name:         aws.String("kubeconfig"),
				SecretString: aws.String("Test"),
			},
		},
		"sec2": {
			GetSecretValueOutput: &secretsmanager.GetSecretValueOutput{
				ARN:          aws.String("arn:aws:secretsmanager:us-east-2:1234567890:secret:kubeconfig-Wtttt"),
				Name:         aws.String("kubeconfig1"),
				SecretBinary: []byte("Test"),
			},
		},
	}
	for _, d := range secrets {
		if aws.StringValue(s.SecretId) == aws.StringValue(d.GetSecretValueOutput.ARN) {
			return d.GetSecretValueOutput, nil
		}
	}
	return nil, fmt.Errorf("Notfound err")
}

func (m *mockSTSClient) GetCallerIdentity(*sts.GetCallerIdentityInput) (*sts.GetCallerIdentityOutput, error) {

	return &sts.GetCallerIdentityOutput{
		Account: aws.String("1234567890"),
		Arn:     aws.String("arn:aws:sts::1234567890:assumed-role/TestRole/session-1587810408"),
		UserId:  aws.String("AROA4OQMRFUSJUBK2CBCZ:session-1587810408"),
	}, nil
}

func (m *mockSTSClient) GetCallerIdentityRequest(*sts.GetCallerIdentityInput) (req *request.Request, output *sts.GetCallerIdentityOutput) {
	op := &request.Operation{
		Name:       "GetCallerIdentity",
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}
	input := &sts.GetCallerIdentityInput{}
	output = &sts.GetCallerIdentityOutput{
		Account: aws.String("1234567890"),
		Arn:     aws.String("arn:aws:sts::1234567890:assumed-role/TestRole/session-1587810408"),
		UserId:  aws.String("AROA4OQMRFUSJUBK2CBCZ:session-1587810408"),
	}
	c := MockSession.ClientConfig("Mock", aws.NewConfig().WithRegion("us-east-2"))
	meta := metadata.ClientInfo{
		ServiceName:   "Mock",
		SigningRegion: c.SigningRegion,
		Endpoint:      c.Endpoint,
		APIVersion:    "2015-12-08",
		JSONVersion:   "1.1",
		TargetPrefix:  "MockServer",
	}
	req = request.New(*c.Config, meta, c.Handlers, nil, op, input, output)
	return
}

func TestGetClusterDetails(t *testing.T) {
	// Setup Test
	mockSvc := &mockEKSClient{}
	clusters := []string{"eks", "eks1", "eks2"}

	for _, cluster := range clusters {
		t.Run(cluster, func(t *testing.T) {
			_, err := getClusterDetails(mockSvc, cluster)
			if err != nil {
				assert.Contains(t, err.Error(), "in unexpected state")
			}
		})
	}
}

func TestGenerateKubeToken(t *testing.T) {
	mockSvc := &mockSTSClient{}
	cluster := aws.String("eks")
	_, err := generateKubeToken(mockSvc, cluster)
	assert.Nil(t, err)
}

func TestGetSecretsManager(t *testing.T) {
	// Setup Test
	mockSvc := &mockSecretsManagerClient{}
	secrets := []string{"arn:aws:secretsmanager:us-east-2:1234567890:secret:kubeconfig-Wt", "arn:aws:secretsmanager:us-east-2:1234567890:secret:kubeconfig-Wtttt", "arn:aws:secretsmanager:us-east-2:1234567890:secret:kubeconfig"}
	expectedErr := "Notfound err"
	//expectedRes := []byte("Test")
	for _, sec := range secrets {
		t.Run(sec, func(t *testing.T) {
			_, err := getSecretsManager(mockSvc, &sec)
			if err != nil {
				assert.Contains(t, err.Error(), expectedErr)
			}
			//assert.Equal(t, res, expectedRes)
		})
	}
}

func TestGetCurrentRoleARN(t *testing.T) {
	// Setup Test
	mockSvc := &mockSTSClient{}
	expectedARN := aws.String("arn:aws:iam::1234567890:role/TestRole")
	expectedErr := "SomeError"
	res, err := getCurrentRoleARN(mockSvc)
	if err != nil {
		assert.Contains(t, err.Error(), expectedErr)
	}
	assert.EqualValues(t, aws.StringValue(expectedARN), aws.StringValue(res))
}

func TestToRoleArn(t *testing.T) {
	arns := []string{"arn:aws:sts::1234567890:assumed-role/TestRole/session-1587810408", "arn:aws:iam::1234567890:role/TestRole"}
	expectedARN := aws.String("arn:aws:iam::1234567890:role/TestRole")
	for _, arn := range arns {
		t.Run(arn, func(t *testing.T) {
			res := toRoleArn(aws.String(arn))
			assert.EqualValues(t, aws.StringValue(expectedARN), aws.StringValue(res))
		})
	}
}
