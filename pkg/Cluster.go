package pkg

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

type Cluster struct {
	resources         []schema.GroupVersionResource
	disco             discovery.DiscoveryInterface
	restConfig        *rest.Config
	kubernetesVersion string
	clientset         dynamic.Interface
	Name              string
	Version           string
}

func NewCluster(kubeconfig string, kubecontext string) *Cluster {
	cluster := Cluster{}
	pathOptions := clientcmd.NewDefaultPathOptions()
	if len(kubeconfig) != 0 {
		pathOptions.GlobalFile = kubeconfig
	}
	config, err := pathOptions.GetStartingConfig()
	if err != nil {
		panic(err)
	}

	configOverrides := clientcmd.ConfigOverrides{}
	if kubecontext != "" {
		configOverrides.CurrentContext = kubecontext
	}

	clientConfig := clientcmd.NewDefaultClientConfig(*config, &configOverrides)
	cluster.restConfig, err = clientConfig.ClientConfig()
	cluster.restConfig.WarningHandler = rest.NoWarnings{}
	if err != nil {
		panic(err)
	}

	if cluster.disco, err = discovery.NewDiscoveryClientForConfig(cluster.restConfig); err != nil {
		panic(err)
	}

	cluster.clientset, err = dynamic.NewForConfig(cluster.restConfig)
	if err != nil {
		panic(err)
	}

	return &cluster
}

func NewClusterFromEnvOrConfig(restConfig *rest.Config) *Cluster {
	cluster := Cluster{}
	defaultRestConfig := &rest.Config{}
	var err error
	useLocalDevMode := os.Getenv("USE_LOCAL_DEV_MODE")
	if useLocalDevMode == "true" {
		usr, err := user.Current()
		if err != nil {
			panic(err)
		}
		kubeconfig := flag.String("kubeconfig", filepath.Join(usr.HomeDir, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
		//flag.Parse()
		defaultRestConfig, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
		if err != nil {
			panic(err)
		}
	} else if restConfig != nil {
		defaultRestConfig = restConfig
	} else {
		defaultRestConfig, err = rest.InClusterConfig()
		if err != nil {
			fmt.Println("error in getting rest config via InClusterConfig")
			panic(err)
		}
	}

	cluster.restConfig = defaultRestConfig
	cluster.restConfig.WarningHandler = rest.NoWarnings{}

	if cluster.disco, err = discovery.NewDiscoveryClientForConfig(cluster.restConfig); err != nil {
		panic(err)
	}

	cluster.clientset, err = dynamic.NewForConfig(cluster.restConfig)
	if err != nil {
		panic(err)
	}

	return &cluster
}

func (c *Cluster) ServerVersion() (string, error) {
	info, err := c.disco.ServerVersion()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.%s", info.Major, strings.Trim(info.Minor, "+")), nil
}
func (c *Cluster) FetchK8sObjects(gvks []schema.GroupVersionKind, conf *Config) []unstructured.Unstructured {
	var resources []schema.GroupVersionResource
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(c.disco))
	var objs []unstructured.Unstructured
	for _, gvk := range gvks {
		if Contains(gvk.Kind, conf.IgnoreKinds) {
			continue
		}
		if len(conf.SelectKinds) > 0 && !Contains(gvk.Kind, conf.SelectKinds) {
			continue
		}
		gvr, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			continue
		}
		resources = append(resources, gvr.Resource)
	}
	for _, resource := range resources {
		if strings.Contains(resource.Resource, "lists") || strings.Contains(resource.Resource, "reviews") || strings.EqualFold(resource.Resource, "bindings") {
			continue
		}
		resInf := c.clientset.Resource(resource)
		objList, err := resInf.List(context.Background(), v1.ListOptions{})
		if err != nil {
			fmt.Printf("err while fetching resource %v error %v\n", resource, err)
			continue
		}
		for _, obj := range objList.Items {
			namespace := obj.GetNamespace()
			if len(obj.GetNamespace()) == 0 {
				namespace = "default"
			}
			if Contains(namespace, conf.IgnoreNamespaces) {
				continue
			}
			if len(conf.SelectNamespaces) > 0 && !Contains(namespace, conf.SelectNamespaces) {
				continue
			}
			objs = append(objs, obj)
		}
	}
	return objs
}
