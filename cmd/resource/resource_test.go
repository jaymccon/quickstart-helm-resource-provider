package resource

import (
	"testing"

	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/stretchr/testify/assert"
)

func TestCreate(t *testing.T) {
	tests := map[string]struct {
		model *Model
	}{
		"WithVPC": {
			model: &Model{
				ClusterID: aws.String("eks"),
				Chart:     aws.String("stable/coscale"),
				Namespace: aws.String("default"),
				VPCConfiguration: &VPCConfiguration{
					SecurityGroupIds: []string{"sg-01"},
					SubnetIds:        []string{"subnet-01"},
				},
			},
		},
		"WithOutVPC": {
			model: &Model{
				ClusterID: aws.String("eks"),
				Chart:     aws.String("stable/coscale"),
				Namespace: aws.String("default"),
			},
		},
	}
	req := handler.Request{
		LogicalResourceID: "TestHelm",
		CallbackContext:   nil,
		Session:           MockSession,
	}

	NewClients = func(cluster *string, kubeconfig *string, namespace *string, ses *session.Session, role *string, customKubeconfig []byte) (*Clients, error) {
		return NewMockClient(t), nil
	}

	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Create(req, &Model{}, d.model)
			assert.Nil(t, err)
		})
	}
}

func TestRead(t *testing.T) {
	tests := map[string]struct {
		model *Model
	}{
		"WithVPC": {
			model: &Model{
				ID: aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoiVGVzdCIsIk5hbWVzcGFjZSI6IlRlc3QifQ"),
				VPCConfiguration: &VPCConfiguration{
					SecurityGroupIds: []string{"sg-01"},
					SubnetIds:        []string{"subnet-01"},
				},
			},
		},
		"WithOutVPC": {
			model: &Model{
				ID:        aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoiVGVzdCIsIk5hbWVzcGFjZSI6IlRlc3QifQ"),
				Namespace: aws.String("default"),
			},
		},
	}
	req := handler.Request{
		LogicalResourceID: "TestHelm",
		CallbackContext:   nil,
		Session:           MockSession,
	}

	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Read(req, &Model{}, d.model)
			assert.Nil(t, err)
		})
	}
}

func TestUpdate(t *testing.T) {
	tests := map[string]struct {
		model *Model
	}{
		"WithVPC": {
			model: &Model{
				ClusterID: aws.String("eks"),
				Chart:     aws.String("stable/coscale"),
				Namespace: aws.String("default"),
				VPCConfiguration: &VPCConfiguration{
					SecurityGroupIds: []string{"sg-01"},
					SubnetIds:        []string{"subnet-01"},
				},
			},
		},
		"WithOutVPC": {
			model: &Model{
				ClusterID: aws.String("eks"),
				Chart:     aws.String("stable/coscale"),
				Namespace: aws.String("default"),
			},
		},
	}
	req := handler.Request{
		LogicalResourceID: "TestHelm",
		CallbackContext:   nil,
		Session:           MockSession,
	}

	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Update(req, &Model{}, d.model)
			assert.Nil(t, err)
		})
	}
}

func TestDelete(t *testing.T) {
	tests := map[string]struct {
		model *Model
	}{
		"WithVPC": {
			model: &Model{
				ClusterID: aws.String("eks"),
				Chart:     aws.String("stable/coscale"),
				Namespace: aws.String("default"),
				VPCConfiguration: &VPCConfiguration{
					SecurityGroupIds: []string{"sg-01"},
					SubnetIds:        []string{"subnet-01"},
				},
			},
		},
		"WithOutVPC": {
			model: &Model{
				ClusterID: aws.String("eks"),
				Chart:     aws.String("stable/coscale"),
				Namespace: aws.String("default"),
			},
		},
	}
	req := handler.Request{
		LogicalResourceID: "TestHelm",
		CallbackContext:   nil,
		Session:           MockSession,
	}

	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Delete(req, &Model{}, d.model)
			assert.Nil(t, err)
		})
	}
}

func TestList(t *testing.T) {
	eError := "Not implemented: List"
	req := handler.Request{
		LogicalResourceID: "TestHelm",
		CallbackContext:   nil,
		Session:           MockSession,
	}
	_, err := List(req, &Model{}, &Model{})
	assert.EqualError(t, err, eError)
}
