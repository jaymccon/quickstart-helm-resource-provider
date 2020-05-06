package resource

import (
	"os"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/stretchr/testify/assert"
)

// TestCreateKubeConfig to test createKubeConfig
func TestCreateKubeConfig(t *testing.T) {
	defer os.Remove(KubeConfigLocalPath)
	mockEKSSvc := &mockEKSClient{}
	mockSTSSvc := &mockSTSClient{}
	mockSMSvc := &mockSecretsManagerClient{}
	tests := map[string]struct {
		cluster, kubeconfig, role *string
		customKubeconfig          []byte
		expectedErr               string
	}{
		"AllValues": {
			cluster:     aws.String("eks"),
			kubeconfig:  aws.String("arn:aws:secretsmanager:us-east-2:1234567890:secret:kubeconfig-Wt"),
			role:        aws.String("arn:aws:iam::1234567890:role/TestRole"),
			expectedErr: "Both ClusterID or KubeConfig can not be specified",
		},
		"OnlyCluster": {
			cluster:     aws.String("eks"),
			expectedErr: "",
		},
		"ClusterWithRole": {
			cluster:     aws.String("eks"),
			role:        aws.String("arn:aws:iam::1234567890:role/TestRole"),
			expectedErr: "",
		},
		"OnlySM": {
			kubeconfig:  aws.String("arn:aws:secretsmanager:us-east-2:1234567890:secret:kubeconfig-Wt"),
			expectedErr: "",
		},
		"NilValues": {
			expectedErr: "Either ClusterID or KubeConfig must be specified",
		},
		"CustomKubeconfig": {
			customKubeconfig: []byte("Test"),
			expectedErr:      "",
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			err := createKubeConfig(mockEKSSvc, mockSTSSvc, mockSMSvc, d.cluster, d.kubeconfig, d.role, d.customKubeconfig)
			if err != nil {
				assert.Contains(t, err.Error(), d.expectedErr)
			} else {
				assert.FileExists(t, KubeConfigLocalPath)
			}
		})
	}
}

// TestCreateNamespace to test createNamespace
func TestCreateNamespace(t *testing.T) {
	c := NewMockClient(t)
	err := c.createNamespace("test")
	assert.NoError(t, err)
}

// TestCheckPendingResources to test CheckPendingResources
func TestCheckPendingResources(t *testing.T) {
	defer os.Remove(TempManifest)
	c := NewMockClient(t)
	rd := &ReleaseData{
		Name:      "test",
		Namespace: "default",
	}
	tests := map[string]struct {
		assertion assert.BoolAssertionFunc
		manifest  string
	}{
		"Pending": {
			assertion: assert.True,
			manifest:  TestPendingManifest,
		},
		"NoPending": {
			assertion: assert.False,
			manifest:  TestManifest,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			rd.Manifest = d.manifest
			result, err := c.CheckPendingResources(rd)
			assert.Nil(t, err)
			d.assertion(t, result)
		})
	}
}

// TestGetKubeResources to test GetKubeResources
func TestGetKubeResources(t *testing.T) {
	defer os.Remove(TempManifest)
	c := NewMockClient(t)
	expectedMap := map[string]interface{}{"Deployment": map[string]interface{}{"nginx-deployment": map[string]interface{}{"ObjectMeta": map[string]interface{}{"Namespace": "default"}, "Status": map[string]interface{}{"AvailableReplicas": "0", "ReadyReplicas": "2", "Replicas": "0"}}, "nginx-ds": map[string]interface{}{"ObjectMeta": map[string]interface{}{"Namespace": "default"}, "Status": map[string]interface{}{"NumberAvailable": "1", "NumberReady": "1", "NumberUnavailable": "0"}}}, "Service": map[string]interface{}{"lb-service": map[string]interface{}{"ObjectMeta": map[string]interface{}{"Namespace": "default"}, "Spec": map[string]interface{}{"ClusterIP": "", "Type": "LoadBalancer"}, "Status": map[string]interface{}{"LoadBalancer": map[string]interface{}{"Ingress": map[string]interface{}{"Hostname": "elb.test.com"}}}}, "my-service": map[string]interface{}{"ObjectMeta": map[string]interface{}{"Namespace": "default"}, "Spec": map[string]interface{}{"Type": ""}}}, "StatefulSet": map[string]interface{}{"nginx-ss": map[string]interface{}{"ObjectMeta": map[string]interface{}{"Namespace": "default"}, "Status": map[string]interface{}{"ReadyReplicas": "2", "Replicas": "0", "UpdatedReplicas": "0"}}}}
	rd := &ReleaseData{
		Name:      "test",
		Namespace: "default",
		Manifest:  TestManifest,
	}
	result, err := c.GetKubeResources(rd)
	assert.Nil(t, err)
	assert.ObjectsAreEqualValues(expectedMap, result)
}

// TestGetManifestDetails to test getManifestDetails
func TestGetManifestDetails(t *testing.T) {
	defer os.Remove(TempManifest)
	c := NewMockClient(t)
	rd := &ReleaseData{
		Name:      "test",
		Namespace: "default",
		Manifest:  TestManifest,
	}
	_, err := c.getManifestDetails(rd)
	assert.Nil(t, err)
}
