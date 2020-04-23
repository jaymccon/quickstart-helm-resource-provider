package resource

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"reflect"

	"helm.sh/helm/v3/pkg/strvals"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	kubeconfigutil "k8s.io/kubernetes/cmd/kubeadm/app/util/kubeconfig"
)

const (
	KubeConfigLocalPath = "/tmp/kubeConfig"
	TempManifest        = "/tmp/manifest.yaml"
	chunkSize           = 500
)

type ReleaseData struct {
	Name, Chart, Namespace, Manifest string `json:",omitempty"`
}

// createKubeConfig create kubeconfig from ClusterID or Secret manager.
func createKubeConfig(esvc EKSAPI, ssvc STSAPI, secsvc SecretsManagerAPI, cluster *string, kubeconfig *string, role *string, customKubeconfig []byte) error {
	switch {
	case cluster != nil:
		defaultConfig := api.NewConfig()
		c, err := getClusterDetails(esvc, *cluster)
		if err != nil {
			return genericError("Getting Cluster details", err)
		}
		defaultConfig.Clusters[*cluster] = &api.Cluster{
			Server:                   c.endpoint,
			CertificateAuthorityData: []byte(c.CAData),
		}
		token, err := generateKubeToken(ssvc, cluster)
		if err != nil {
			return err
		}
		defaultConfig.AuthInfos["aws"] = &api.AuthInfo{
			Token: *token,
		}
		defaultConfig.Contexts["aws"] = &api.Context{
			Cluster:  *cluster,
			AuthInfo: "aws",
		}
		defaultConfig.CurrentContext = "aws"
		log.Printf("Writing kubeconfig file to %s", KubeConfigLocalPath)

		err = kubeconfigutil.WriteToDisk(KubeConfigLocalPath, defaultConfig)
		if err != nil {
			return genericError("Write file: ", err)
		}
		return nil
	case kubeconfig != nil:
		s, err := getSecretsManager(secsvc, kubeconfig)
		if err != nil {
			return err
		}
		log.Printf("Writing kubeconfig file to %s", KubeConfigLocalPath)
		err = ioutil.WriteFile(KubeConfigLocalPath, s, 0600)
		if err != nil {
			return genericError("Write file: ", err)
		}
		return nil
	case customKubeconfig != nil:
		log.Printf("Writing kubeconfig file to %s", KubeConfigLocalPath)
		err := ioutil.WriteFile(KubeConfigLocalPath, customKubeconfig, 0600)
		if err != nil {
			return genericError("Write file: ", err)
		}
		return nil
	default:
		return errors.New("Either ClusterID or KubeConfig must be specified")
	}
}

// kubeClient create kube client from kubeconfig file.
func kubeClient() (kubernetes.Interface, *rest.Config, error) {
	config, err := clientcmd.BuildConfigFromFlags("", KubeConfigLocalPath)
	if err != nil {
		return nil, nil, genericError("Process Kubeconfig", err)
	}

	// create the clientset
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, genericError("Creating Clientset", err)
	}
	return clientSet, config, nil
}

// createNamespace create NS if not exists
func (c *Clients) createNamespace(namespace string) error {
	nsSpec := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	_, err := c.ClientSet.CoreV1().Namespaces().Create(nsSpec)
	log.Println(err)
	switch err {
	case nil:
		return nil
	default:
		switch kerrors.IsAlreadyExists(err) {
		case true:
			log.Printf("Namespace : %s. Already exists. Continue to install...", namespace)
			return nil
		default:
			return genericError("Create NS", err)
		}
	}
}

// CheckPendingResources checks pending resources in for the specific release.
func (c *Clients) CheckPendingResources(r *ReleaseData) (bool, error) {
	log.Printf("Checking pending resources in %s", r.Name)
	if r.Manifest == "" {
		return true, errors.New("Manifest not provided in the request")
	}
	pending := false
	infos, err := c.getManifestDetails(r)
	if err != nil {
		return true, err
	}
	for _, info := range infos {
		kind := info.Object.GetObjectKind().GroupVersionKind().GroupKind().Kind
		data, err := json.Marshal(info.Object)
		if err != nil {
			return true, err
		}

		switch kind {
		case "Service":
			var svc v1.Service
			if err := json.Unmarshal(data, &svc); err != nil {
				return true, err
			}
			switch svc.Spec.Type {
			case "LoadBalancer":
				if reflect.ValueOf(svc.Status.LoadBalancer.Ingress).Len() <= 0 {
					pending = true
				}
			}
		case "ReplicaSet":
			var r appsv1.ReplicaSet
			if err := json.Unmarshal(data, &r); err != nil {
				return true, err
			}
			if r.Status.ReadyReplicas < *r.Spec.Replicas {
				pending = true
			}
		case "Deployment":
			var d appsv1.Deployment
			if err := json.Unmarshal(data, &d); err != nil {
				return true, err
			}
			if d.Status.ReadyReplicas < *d.Spec.Replicas {
				pending = true
			}
		case "DaemonSet":
			var d appsv1.DaemonSet
			if err := json.Unmarshal(data, &d); err != nil {
				return true, err
			}
			if d.Status.NumberUnavailable > 0 {
				pending = true
			}

		case "StatefulSet":
			var d appsv1.StatefulSet
			if err := json.Unmarshal(data, &d); err != nil {
				return true, err
			}
			if d.Status.ReadyReplicas < *d.Spec.Replicas {
				pending = true
			}
		case "Ingress":
			var i v1beta1.Ingress
			if err := json.Unmarshal(data, &i); err != nil {
				return true, err
			}
			if reflect.ValueOf(i.Status.LoadBalancer.Ingress).Len() <= 0 {
				pending = true
			}
		}
	}
	return pending, nil
}

// GetKubeResources get resources for the specific release.
func (c *Clients) GetKubeResources(r *ReleaseData) (map[string]interface{}, error) {
	log.Printf("Getting resources for %s", r.Name)
	if r.Manifest == "" {
		return nil, errors.New("Manifest not provided in the request")
	}
	resources := make(map[string]interface{})
	infos, err := c.getManifestDetails(r)
	if err != nil {
		return nil, err
	}
	for _, info := range infos {
		kind := info.Object.GetObjectKind().GroupVersionKind().GroupKind().Kind
		data, err := json.Marshal(info.Object)
		if err != nil {
			return nil, err
		}
		switch kind {
		case "Service":
			var svc v1.Service
			if err := json.Unmarshal(data, &svc); err != nil {
				return nil, err
			}
			if err := strvals.ParseIntoString(fmt.Sprintf("Service.%s.ObjectMeta.Namespace=%s", svc.Name, svc.ObjectMeta.Namespace), resources); err != nil {
				return nil, err
			}
			if err := strvals.ParseIntoString(fmt.Sprintf("Service.%s.Spec.Type=%s", svc.Name, svc.Spec.Type), resources); err != nil {
				return nil, err
			}
			switch svc.Spec.Type {
			case "LoadBalancer":
				if reflect.ValueOf(svc.Status.LoadBalancer.Ingress).Len() > 0 {
					if err := strvals.ParseIntoString(fmt.Sprintf("Service.%s.Status.LoadBalancer.Ingress.Hostname=%s", svc.Name, svc.Status.LoadBalancer.Ingress[0].Hostname), resources); err != nil {
						return nil, err
					}
				}
				if err := strvals.ParseIntoString(fmt.Sprintf("Service.%s.Spec.ClusterIP=%s", svc.Name, svc.Spec.ClusterIP), resources); err != nil {
					return nil, err
				}
			case "ClusterIP":
				if err := strvals.ParseIntoString(fmt.Sprintf("Service.%s.Spec.ClusterIP=%s", svc.Name, svc.Spec.ClusterIP), resources); err != nil {
					return nil, err
				}

			case "ExternalName":
				if err := strvals.ParseIntoString(fmt.Sprintf("Service.%s.Spec.ExternalName=%s", svc.Name, svc.Spec.ExternalName), resources); err != nil {
					return nil, err
				}
			}
		case "Deployment":
			var d appsv1.Deployment
			if err := json.Unmarshal(data, &d); err != nil {
				return nil, err
			}

			if err := strvals.ParseIntoString(fmt.Sprintf("Deployment.%s.ObjectMeta.Namespace=%s", d.ObjectMeta.Name, d.ObjectMeta.Namespace), resources); err != nil {
				return nil, err
			}
			if err := strvals.ParseIntoString(fmt.Sprintf("Deployment.%s.Status.Replicas=%d", d.ObjectMeta.Name, d.Status.Replicas), resources); err != nil {
				return nil, err
			}
			if err := strvals.ParseIntoString(fmt.Sprintf("Deployment.%s.Status.ReadyReplicas=%d", d.ObjectMeta.Name, d.Status.ReadyReplicas), resources); err != nil {
				return nil, err
			}
			if err := strvals.ParseIntoString(fmt.Sprintf("Deployment.%s.Status.AvailableReplicas=%d", d.ObjectMeta.Name, d.Status.AvailableReplicas), resources); err != nil {
				return nil, err
			}
		case "DaemonSet":
			var d appsv1.DaemonSet
			if err := json.Unmarshal(data, &d); err != nil {
				return nil, err
			}
			if err := strvals.ParseIntoString(fmt.Sprintf("Deployment.%s.ObjectMeta.Namespace=%s", d.ObjectMeta.Name, d.ObjectMeta.Namespace), resources); err != nil {
				return nil, err
			}
			if err := strvals.ParseIntoString(fmt.Sprintf("Deployment.%s.Status.NumberReady=%d", d.ObjectMeta.Name, d.Status.NumberReady), resources); err != nil {
				return nil, err
			}
			if err := strvals.ParseIntoString(fmt.Sprintf("Deployment.%s.Status.NumberAvailable=%d", d.ObjectMeta.Name, d.Status.NumberAvailable), resources); err != nil {
				return nil, err
			}
			if err := strvals.ParseIntoString(fmt.Sprintf("Deployment.%s.Status.NumberUnavailable=%d", d.ObjectMeta.Name, d.Status.NumberUnavailable), resources); err != nil {
				return nil, err
			}
		case "StatefulSet":
			var d appsv1.StatefulSet
			if err := json.Unmarshal(data, &d); err != nil {
				return nil, err
			}
			if err := strvals.ParseIntoString(fmt.Sprintf("StatefulSet.%s.ObjectMeta.Namespace=%s", d.ObjectMeta.Name, d.ObjectMeta.Namespace), resources); err != nil {
				return nil, err
			}
			if err := strvals.ParseIntoString(fmt.Sprintf("StatefulSet.%s.Status.Replicas=%d", d.ObjectMeta.Name, d.Status.Replicas), resources); err != nil {
				return nil, err
			}
			if err := strvals.ParseIntoString(fmt.Sprintf("StatefulSet.%s.Status.ReadyReplicas=%d", d.ObjectMeta.Name, d.Status.ReadyReplicas), resources); err != nil {
				return nil, err
			}
			if err := strvals.ParseIntoString(fmt.Sprintf("StatefulSet.%s.Status.UpdatedReplicas=%d", d.ObjectMeta.Name, d.Status.UpdatedReplicas), resources); err != nil {
				return nil, err
			}
		case "Ingress":
			var i v1beta1.Ingress
			if err := json.Unmarshal(data, &i); err != nil {
				return nil, err
			}
			if reflect.ValueOf(i.Status.LoadBalancer.Ingress).Len() > 0 {
				if err := strvals.ParseIntoString(fmt.Sprintf("Ingresses.%s.Status.LoadBalancer.Ingress.Hostname=%s", i.Name, i.Status.LoadBalancer.Ingress[0].Hostname), resources); err != nil {
					return nil, err
				}
			}
		default:
			var dat map[string]interface{}
			if err := json.Unmarshal(data, &dat); err != nil {
				return nil, err
			}
			metadata := dat["metadata"].(map[string]interface{})
			if err := strvals.ParseIntoString(fmt.Sprintf("%s.%s.ObjectMeta.Namespace=%s", kind, metadata["name"], metadata["namespace"]), resources); err != nil {
				return nil, err
			}
		}
	}
	return resources, nil
}

func (c *Clients) getManifestDetails(r *ReleaseData) ([]*resource.Info, error) {
	log.Printf("Getting resources for %s's manifest", r.Name)

	err := ioutil.WriteFile(TempManifest, []byte(r.Manifest), 0600)
	if err != nil {
		return nil, genericError("Write manifest file: ", err)
	}

	f := &resource.FilenameOptions{
		Filenames: []string{TempManifest},
	}
	o := resource.NewBuilder(c.RestClientGetter)
	res := o.
		Unstructured().
		NamespaceParam(r.Namespace).DefaultNamespace().AllNamespaces(false).
		FilenameParam(false, f).
		RequestChunksOf(chunkSize).
		ContinueOnError().
		Latest().
		Flatten().
		TransformRequests().
		Do()

	infos, err := res.Infos()
	if err != nil {
		return nil, err
	}
	return infos, nil
}
