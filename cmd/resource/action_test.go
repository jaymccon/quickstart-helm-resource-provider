package resource

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/stretchr/testify/assert"
)

func TestInitialize(t *testing.T) {
	m := &Model{
		ClusterID: aws.String("eks"),
		Chart:     aws.String("stable/coscale"),
		Namespace: aws.String("default"),
	}
	vpc := &VPCConfiguration{
		SecurityGroupIds: []string{"sg-01"},
		SubnetIds:        []string{"subnet-01"},
	}
	data := []byte("Test")
	_ = ioutil.WriteFile(KubeConfigLocalPath, data, 0644)
	_ = ioutil.WriteFile(ZipFile, data, 0644)
	defer os.Remove(KubeConfigLocalPath)
	defer os.Remove(ZipFile)
	tests := map[string]struct {
		action    Action
		vpc       bool
		name      string
		nextStage Stage
	}{
		"InstallWithVPC": {
			action:    InstallReleaseAction,
			name:      "Test",
			vpc:       true,
			nextStage: ReleaseStabilize,
		},
		"InstallWithOutVPC": {
			action:    InstallReleaseAction,
			name:      "Test",
			vpc:       false,
			nextStage: ReleaseStabilize,
		},
		"UpdateWithOutVPC": {
			action:    UpdateReleaseAction,
			name:      "one",
			vpc:       false,
			nextStage: ReleaseStabilize,
		},
		"UpdateWithVPC": {
			action:    UpdateReleaseAction,
			name:      "one",
			vpc:       true,
			nextStage: ReleaseStabilize,
		},
		"UninstallsWithOutVPC": {
			action:    UninstallReleaseAction,
			name:      "one",
			vpc:       false,
			nextStage: CompleteStage,
		},
		"UninstallWithVPC": {
			action:    UninstallReleaseAction,
			name:      "one",
			vpc:       true,
			nextStage: CompleteStage,
		},
	}

	NewClients = func(cluster *string, kubeconfig *string, namespace *string, ses *session.Session, role *string, customKubeconfig []byte) (*Clients, error) {
		return NewMockClient(t), nil
	}

	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			if d.vpc {
				m.VPCConfiguration = vpc
			}
			m.Name = aws.String(d.name)
			eRes := makeEvent(m, d.nextStage, nil)
			res := initialize(MockSession, m, d.action)
			assert.EqualValues(t, eRes, res)
		})
	}
}

func TestCheckReleaseStatus(t *testing.T) {
	m := &Model{
		ClusterID: aws.String("eks"),
		ID:        aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoiVGVzdCIsIk5hbWVzcGFjZSI6IlRlc3QifQ"),
	}
	vpc := &VPCConfiguration{
		SecurityGroupIds: []string{"sg-01"},
		SubnetIds:        []string{"subnet-01"},
	}
	c := NewMockClient(t)
	data := []byte("Test")
	_ = ioutil.WriteFile(KubeConfigLocalPath, data, 0644)
	_ = ioutil.WriteFile(ZipFile, data, 0644)
	defer os.Remove(KubeConfigLocalPath)
	defer os.Remove(ZipFile)
	tests := map[string]struct {
		vpc       bool
		name      *string
		nextStage Stage
	}{
		"WithVPC": {
			name:      aws.String("one"),
			vpc:       true,
			nextStage: CompleteStage,
		},
		"WithOutVPC": {
			name:      aws.String("one"),
			vpc:       false,
			nextStage: CompleteStage,
		},
	}

	NewClients = func(cluster *string, kubeconfig *string, namespace *string, ses *session.Session, role *string, customKubeconfig []byte) (*Clients, error) {
		return NewMockClient(t), nil
	}

	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			if d.vpc {
				m.VPCConfiguration = vpc
			}
			m.Name = d.name
			eRes := makeEvent(m, d.nextStage, nil)
			res := checkReleaseStatus(c.AWSClients.Session(nil, nil), m, d.nextStage)
			assert.EqualValues(t, eRes, res)
		})
	}
}
func TestLambdaDestroy(t *testing.T) {
	expected := handler.ProgressEvent{}
	m := &Model{
		ClusterID: aws.String("eks"),
		VPCConfiguration: &VPCConfiguration{
			SecurityGroupIds: []string{"sg-1"},
			SubnetIds:        []string{"subnet-1"},
		},
	}
	c := NewMockClient(t)
	result := c.lambdaDestroy(m)
	assert.EqualValues(t, expected, result)

}
