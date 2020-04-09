package resource

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"reflect"

	"github.com/aws/aws-sdk-go/aws/session"
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
	kubeConfigLocalPath = "/tmp/kubeConfig"
	tempManifest        = "/tmp/manifest.yaml"
	chunkSize           = 500
)

type releaseData struct {
	name, chart, namespace, manifest string
}

// createKubeConfig create kubeconfig from ClusterID or Secret manager.
func createKubeConfig(session *session.Session, cluster *string, kubeconfig *string) error {
	switch {
	case cluster != nil:
		defaultConfig := api.NewConfig()
		c, err := getClusterDetails(session, *cluster)
		if err != nil {
			return genericError("Getting Cluster details", err)
		}
		defaultConfig.Clusters[*cluster] = &api.Cluster{
			Server:                   c.endpoint,
			CertificateAuthorityData: []byte(c.CAData),
		}
		token, err := generateKubeToken(session, *cluster)
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
		log.Printf("Writing kubeconfig file to %s", kubeConfigLocalPath)

		err = kubeconfigutil.WriteToDisk(kubeConfigLocalPath, defaultConfig)
		if err != nil {
			return genericError("Write file: ", err)
		}
		return nil
	case kubeconfig != nil:
		s, err := getSecretsManager(session, kubeconfig)
		if err != nil {
			return err
		}
		log.Printf("Writing kubeconfig file to %s", kubeConfigLocalPath)
		err = ioutil.WriteFile(kubeConfigLocalPath, s, 0600)
		if err != nil {
			return genericError("Write file: ", err)
		}
		return nil
	default:
		return errors.New("Either ClusterID or KubeConfig must be specified")
	}
}

// kubeClient create kube client from kubeconfig file.
func kubeClient() (*kubernetes.Clientset, *rest.Config, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfigLocalPath)
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
func (c *Client) createNamespace(namespace string) error {
	nsSpec := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	_, err := c.clientSet.CoreV1().Namespaces().Create(nsSpec)
	if kerrors.IsAlreadyExists(err) {
		log.Printf("Namespace : %s. Already exists. Continue to install...", namespace)
	} else if err != nil {
		return genericError("Create NS", err)
	}
	return nil
}

// checkPendingResources checks pending resources in for the specific release.
func (c *Client) checkPendingResources(r *releaseData) (bool, error) {
	log.Printf("Checking pending resources in %s", r.name)
	if r.manifest == "" {
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

// getKubeResources get resources for the specific release.
func (c *Client) getKubeResources(r *releaseData) (map[string]interface{}, error) {
	log.Printf("Getting resources for %s", r.name)
	if r.manifest == "" {
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

func (c *Client) getManifestDetails(r *releaseData) ([]*resource.Info, error) {
	log.Printf("Getting resources for %s's manifest", r.name)

	err := ioutil.WriteFile(tempManifest, []byte(r.manifest), 0600)
	if err != nil {
		return nil, genericError("Write manifest file: ", err)
	}

	f := &resource.FilenameOptions{
		Filenames: []string{tempManifest},
	}
	o := resource.NewBuilder(c.settings.RESTClientGetter())
	res := o.
		Unstructured().
		NamespaceParam(r.namespace).DefaultNamespace().AllNamespaces(false).
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
