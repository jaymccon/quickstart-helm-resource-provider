package resource

import (
	"bytes"
	"github.com/aws/aws-sdk-go/aws/client/metadata"
	"github.com/aws/aws-sdk-go/aws/request"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	htime "helm.sh/helm/v3/pkg/time"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/meta/testrestmapper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/discovery"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest/fake"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
)

type chartOptions struct {
	*chart.Chart
}

type chartOption func(*chartOptions)

type fakeCachedDiscoveryClient struct {
	discovery.DiscoveryInterface
}

var (
	TestFolder         = "testdata"
	TestZipFile        = TestFolder + "/test_lambda.zip"
	grace              = int64(30)
	enableServiceLinks = corev1.DefaultEnableServiceLinks
)

// Session is a mock session which is used to hit the mock server
var MockSession = func() *session.Session {
	// server is the mock server that simply writes a 200 status back to the client
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	return session.Must(session.NewSession(&aws.Config{
		DisableSSL: aws.Bool(true),
		Endpoint:   aws.String(server.URL),
		Region:     aws.String("us-east-1"),
	}))
}()

var TestManifest = `---
apiVersion: apps/v1
kind: Deployment
metadata:
 name: nginx-deployment

---
apiVersion: v1
kind: Service
metadata:
 name: my-service

---
apiVersion: v1
kind: Service
metadata:
 name: lb-service
 spec:
  type: LoadBalancer

---
apiVersion: apps/v1
kind: DaemonSet
metadata:
 name: nginx-ds

---
apiVersion: apps/v1
kind: StatefulSet
metadata:
 name: nginx-ss

---
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: test-ingress

---
apiVersion: v1
kind: ConfigMap
metadata:
  name: game-demo`

var TestPendingManifest = `apiVersion: apps/v1
kind: Deployment
metadata:
 name: nginx-deployment-foo`

func newFakeBuilder(t *testing.T) func() *resource.Builder {
	cfg, _ := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	clientConfig := clientcmd.NewDefaultClientConfig(*cfg, &clientcmd.ConfigOverrides{})
	configFlags := genericclioptions.NewTestConfigFlags().
		WithClientConfig(clientConfig).
		WithRESTMapper(testRESTMapper())
	header := http.Header{}
	header.Set("Content-Type", runtime.ContentTypeJSON)
	codec := scheme.Codecs.LegacyCodec(scheme.Scheme.PrioritizedVersionsAllGroups()...)
	td := testKubeData()
	return func() *resource.Builder {
		return resource.NewFakeBuilder(
			func(version schema.GroupVersion) (resource.RESTClient, error) {
				return &fake.RESTClient{
					GroupVersion:         schema.GroupVersion{Version: "v1"},
					NegotiatedSerializer: resource.UnstructuredPlusDefaultContentConfig().NegotiatedSerializer,
					Client: fake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
						switch p, m := req.URL.Path, req.Method; {
						case p == "/namespaces/test/services" && m == "POST":
							return &http.Response{StatusCode: http.StatusCreated, Header: header, Body: ObjBody(codec, td.ns)}, nil
						case p == "/namespaces/default/deployments/nginx-deployment" && m == "GET":
							return &http.Response{StatusCode: http.StatusOK, Header: header, Body: ObjBody(codec, td.dep)}, nil
						case p == "/namespaces/default/deployments/nginx-deployment-foo" && m == "GET":
							return &http.Response{StatusCode: http.StatusOK, Header: header, Body: ObjBody(codec, td.pdep)}, nil
						case p == "/namespaces/default/services/my-service" && m == "GET":
							return &http.Response{StatusCode: http.StatusOK, Header: header, Body: ObjBody(codec, td.svc)}, nil
						case p == "/namespaces/default/services/lb-service" && m == "GET":
							return &http.Response{StatusCode: http.StatusOK, Header: header, Body: ObjBody(codec, td.lsvc)}, nil
						case p == "/namespaces/default/daemonsets/nginx-ds" && m == "GET":
							return &http.Response{StatusCode: http.StatusOK, Header: header, Body: ObjBody(codec, td.ds)}, nil
						case p == "/namespaces/default/statefulsets/nginx-ss" && m == "GET":
							return &http.Response{StatusCode: http.StatusOK, Header: header, Body: ObjBody(codec, td.ss)}, nil
						case p == "/ingress/test-ingress" && m == "GET":
							return &http.Response{StatusCode: http.StatusOK, Header: header, Body: ObjBody(codec, td.ing)}, nil
						case p == "/namespaces/default/configmaps/game-demo" && m == "GET":
							return &http.Response{StatusCode: http.StatusOK, Header: header, Body: ObjBody(codec, td.cm)}, nil
						default:
							t.Fatalf("unexpected request: %#v\n%#v", req.URL, req)
							return nil, nil
						}
					}),
				}, nil
			},
			configFlags.ToRESTMapper,
			func() (restmapper.CategoryExpander, error) {
				return resource.FakeCategoryExpander, nil
			},
		)
	}
}

type mockAWSClients struct {
	AWSSession *session.Session
	AWSClientsIface
}

func NewMockClient(t *testing.T) *Clients {
	t.Helper()
	h := ActionConfigFixture(t)
	makeMeSomeReleases(h.Releases, t)
	return &Clients{
		ResourceBuilder: newFakeBuilder(t),
		ClientSet:       fakeclientset.NewSimpleClientset(),
		HelmClient:      h,
		Settings:        cli.New(),
		AWSClients:      &mockAWSClients{AWSSession: MockSession},
	}
}

func ObjBody(codec runtime.Codec, obj runtime.Object) io.ReadCloser {
	return ioutil.NopCloser(bytes.NewReader([]byte(runtime.EncodeOrDie(codec, obj))))
}

func testRESTMapper() meta.RESTMapper {
	groupResources := testDynamicResources()
	mapper := restmapper.NewDiscoveryRESTMapper(groupResources)
	// for backwards compatibility with existing tests, allow rest mappings from the scheme to show up
	// TODO: make this opt-in?
	mapper = meta.FirstHitRESTMapper{
		MultiRESTMapper: meta.MultiRESTMapper{
			mapper,
			testrestmapper.TestOnlyStaticRESTMapper(legacyscheme.Scheme),
		},
	}

	fakeDs := &fakeCachedDiscoveryClient{}
	expander := restmapper.NewShortcutExpander(mapper, fakeDs)
	return expander
}

func testDynamicResources() []*restmapper.APIGroupResources {
	return []*restmapper.APIGroupResources{
		{
			Group: metav1.APIGroup{
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1": {
					{Name: "pods", Namespaced: true, Kind: "Pod"},
					{Name: "services", Namespaced: true, Kind: "Service"},
					{Name: "replicationcontrollers", Namespaced: true, Kind: "ReplicationController"},
					{Name: "componentstatuses", Namespaced: false, Kind: "ComponentStatus"},
					{Name: "nodes", Namespaced: false, Kind: "Node"},
					{Name: "secrets", Namespaced: true, Kind: "Secret"},
					{Name: "configmaps", Namespaced: true, Kind: "ConfigMap"},
					{Name: "namespacedtype", Namespaced: true, Kind: "NamespacedType"},
					{Name: "namespaces", Namespaced: false, Kind: "Namespace"},
					{Name: "resourcequotas", Namespaced: true, Kind: "ResourceQuota"},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "extensions",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1beta1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1beta1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1beta1": {
					{Name: "deployments", Namespaced: true, Kind: "Deployment"},
					{Name: "replicasets", Namespaced: true, Kind: "ReplicaSet"},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "apps",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1beta1"},
					{Version: "v1beta2"},
					{Version: "v1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1beta1": {
					{Name: "deployments", Namespaced: true, Kind: "Deployment"},
					{Name: "replicasets", Namespaced: true, Kind: "ReplicaSet"},
				},
				"v1beta2": {
					{Name: "deployments", Namespaced: true, Kind: "Deployment"},
				},
				"v1": {
					{Name: "deployments", Namespaced: true, Kind: "Deployment"},
					{Name: "replicasets", Namespaced: true, Kind: "ReplicaSet"},
					{Name: "statefulsets", Namespaced: true, Kind: "StatefulSet"},
					{Name: "daemonsets", Namespaced: true, Kind: "DaemonSet"},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "networking.k8s.io",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1beta1"},
					{Version: "v0"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1beta1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1beta1": {
					{Name: "ingress", Namespaced: false, Kind: "Ingress"},
				},
			},
		},
	}
}

func ActionConfigFixture(t *testing.T) *action.Configuration {
	t.Helper()
	var verbose = aws.Bool(false)
	return &action.Configuration{
		Releases:     storage.Init(driver.NewMemory()),
		KubeClient:   &kubefake.FailingKubeClient{PrintingKubeClient: kubefake.PrintingKubeClient{Out: ioutil.Discard}},
		Capabilities: chartutil.DefaultCapabilities,
		Log: func(format string, v ...interface{}) {
			t.Helper()
			if *verbose {
				t.Logf(format, v...)
			}
		},
	}
}

func awsRequest(op *request.Operation, input, output interface{}) *request.Request {
	c := MockSession.ClientConfig("Mock", aws.NewConfig().WithRegion("us-east-2"))
	meta := metadata.ClientInfo{
		ServiceName:   "Mock",
		SigningRegion: c.SigningRegion,
		Endpoint:      c.Endpoint,
		APIVersion:    "2015-12-08",
		JSONVersion:   "1.1",
		TargetPrefix:  "MockServer",
	}
	return request.New(*c.Config, meta, c.Handlers, nil, op, input, output)
}

func makeMeSomeReleases(store *storage.Storage, t *testing.T) {
	t.Helper()
	one := namedRelease("one", release.StatusDeployed)
	one.Namespace = "default"
	one.Version = 1
	one.Manifest = TestManifest
	two := namedRelease("two", release.StatusFailed)
	two.Namespace = "default"
	two.Version = 2
	two.Manifest = TestManifest
	three := namedRelease("three", release.StatusDeployed)
	three.Namespace = "default"
	three.Version = 3
	three.Manifest = TestPendingManifest
	four := namedRelease("four", "unknown)")
	four.Namespace = "default"
	four.Version = 3
	four.Manifest = TestManifest
	five := namedRelease("five", release.StatusPendingUpgrade)
	five.Namespace = "default"
	five.Version = 3
	five.Manifest = TestManifest

	for _, rel := range []*release.Release{one, two, three, four, five} {
		if err := store.Create(rel); err != nil {
			t.Fatal(err)
		}
	}
}

func namedRelease(name string, status release.Status) *release.Release {
	now := htime.Now()
	return &release.Release{
		Name: name,
		Info: &release.Info{
			FirstDeployed: now,
			LastDeployed:  now,
			Status:        status,
			Description:   "Named Release Stub",
		},
		Chart:   buildChart(),
		Version: 1,
	}
}

func buildChart(opts ...chartOption) *chart.Chart {
	c := &chartOptions{
		Chart: &chart.Chart{
			// TODO: This should be more complete.
			Metadata: &chart.Metadata{
				APIVersion: "v1",
				Name:       "hello",
				Version:    "0.1.0",
			},
			// This adds a basic template and hooks.
			Templates: []*chart.File{
				{Name: "templates/temp", Data: []byte(TestManifest)},
			},
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c.Chart
}

type td struct {
	ns   *v1.Namespace
	dep  *appsv1.Deployment
	pdep *appsv1.Deployment
	svc  *v1.Service
	lsvc *v1.Service
	ds   *appsv1.DaemonSet
	ss   *appsv1.StatefulSet
	ing  *v1beta1.Ingress
	cm *v1.ConfigMap
}

func testKubeData() *td {
	t := &td{}
	t.ns = &v1.Namespace{}
	t.ns.Name = "test"

	t.dep = &appsv1.Deployment{}
	t.dep.Name = "nginx-deployment"
	t.dep.Status.ReadyReplicas = int32(2)
	t.dep.Spec.Replicas = aws.Int32(2)

	t.pdep = &appsv1.Deployment{}
	t.pdep.Name = "nginx-deployment-foo"
	t.pdep.Status.ReadyReplicas = int32(1)
	t.pdep.Spec.Replicas = aws.Int32(2)

	t.svc = &v1.Service{}
	t.svc.Name = "my-service"

	t.lsvc = &v1.Service{}
	t.lsvc.Name = "lb-service"
	t.lsvc.Spec.Type = v1.ServiceTypeLoadBalancer
	t.lsvc.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{v1.LoadBalancerIngress{Hostname: "elb.test.com"}}

	t.ds = &appsv1.DaemonSet{}
	t.ds.Name = "nginx-ds"
	t.ds.Status.NumberUnavailable = int32(0)
	t.ds.Status.NumberReady = int32(1)
	t.ds.Status.NumberAvailable = int32(1)

	t.ss = &appsv1.StatefulSet{}
	t.ss.Name = "nginx-ss"
	t.ss.Status.ReadyReplicas = int32(2)
	t.ss.Spec.Replicas = aws.Int32(2)

	t.ing = &v1beta1.Ingress{}
	t.ing.Name = "test-ingress"
	t.ing.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{v1.LoadBalancerIngress{Hostname: "ingress.test.com"}}

	t.cm = &v1.ConfigMap{}
	t.cm.Name = "game-demo"

	return t
}
