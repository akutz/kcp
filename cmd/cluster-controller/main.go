package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	crdexternalversions "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/genericcontrolplane/clientutils"

	kcpclient "github.com/kcp-dev/kcp/pkg/client/clientset/versioned"
	kcpexternalversions "github.com/kcp-dev/kcp/pkg/client/informers/externalversions"
	"github.com/kcp-dev/kcp/pkg/reconciler/apiresource"
	"github.com/kcp-dev/kcp/pkg/reconciler/cluster"
)

const numThreads = 2
const resyncPeriod = 10 * time.Hour

var (
	kubeconfigPath  = flag.String("kubeconfig", "", "Path to kubeconfig")
	syncerImage     = flag.String("syncer_image", "", "Syncer image to install on clusters")
	pullMode        = flag.Bool("pull_mode", true, "Deploy the syncer in registered physical clusters in POD, and have it sync resources from KCP")
	pushMode        = flag.Bool("push_mode", false, "If true, run syncer for each cluster from inside cluster controller")
	autoPublishAPIs = flag.Bool("auto_publish_apis", false, "If true, the APIs imported from physical clusters will be published automatically as CRDs")
)

func main() {
	// Setup signal handler for a cleaner shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Kill, os.Interrupt)
	defer cancel()

	flag.Parse()

	configLoader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: *kubeconfigPath},
		&clientcmd.ConfigOverrides{})

	r, err := configLoader.ClientConfig()
	if err != nil {
		klog.Fatal(err)
	}
	clientutils.EnableMultiCluster(r, nil, "clusters", "customresourcedefinitions", "apiresourceimports", "negotiatedapiresources")
	kubeconfig, err := configLoader.RawConfig()
	if err != nil {
		klog.Fatal(err)
	}

	resourcesToSync := flag.Args()
	if len(resourcesToSync) == 0 {
		resourcesToSync = []string{"deployments.apps"}
	}
	klog.Infof("Syncing resources: %v", resourcesToSync)

	if *pullMode && *pushMode {
		klog.Fatal("can't set --push_mode and --pull_mode")
	}
	syncerMode := cluster.SyncerModeNone
	if *pullMode {
		syncerMode = cluster.SyncerModePull
	}
	if *pushMode {
		syncerMode = cluster.SyncerModePush
	}

	kcpSharedInformerFactory := kcpexternalversions.NewSharedInformerFactoryWithOptions(kcpclient.NewForConfigOrDie(r), resyncPeriod)
	crdSharedInformerFactory := crdexternalversions.NewSharedInformerFactoryWithOptions(apiextensionsclient.NewForConfigOrDie(r), resyncPeriod)

	apiExtensionsClient := apiextensionsclient.NewForConfigOrDie(r)
	kcpClient := kcpclient.NewForConfigOrDie(r)

	clusterController, err := cluster.NewController(
		apiExtensionsClient,
		kcpClient,
		kcpSharedInformerFactory.Cluster().V1alpha1().Clusters(),
		kcpSharedInformerFactory.Apiresource().V1alpha1().APIResourceImports(),
		*syncerImage,
		kubeconfig,
		resourcesToSync,
		syncerMode,
	)
	if err != nil {
		klog.Fatal(err)
	}

	apiresourceController, err := apiresource.NewController(
		apiExtensionsClient,
		kcpClient,
		*autoPublishAPIs,
		kcpSharedInformerFactory.Apiresource().V1alpha1().NegotiatedAPIResources(),
		kcpSharedInformerFactory.Apiresource().V1alpha1().APIResourceImports(),
		crdSharedInformerFactory.Apiextensions().V1().CustomResourceDefinitions(),
	)
	if err != nil {
		klog.Fatal(err)
	}

	kcpSharedInformerFactory.Start(ctx.Done())
	kcpSharedInformerFactory.WaitForCacheSync(ctx.Done())

	crdSharedInformerFactory.Start(ctx.Done())
	crdSharedInformerFactory.WaitForCacheSync(ctx.Done())

	clusterController.Start(ctx, numThreads)
	apiresourceController.Start(ctx, 2)

	<-ctx.Done()
}
