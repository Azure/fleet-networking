package apiclient

import (
	"go.goms.io/fleet-networking/pkg/generated/clientset/internalclientset"
	"istio.io/istio/pkg/kube"
	"k8s.io/client-go/rest"
)

var _ Client = (*client)(nil)

type Client interface {
	kube.Client
	Core() kube.Client
	Networking() internalclientset.Interface
}

type client struct {
	kube.Client
	networking internalclientset.Interface
}

func New(restConfig *rest.Config) (*client, error) {
	restCfg := kube.NewClientConfigForRestConfig(restConfig)
	kubeClient, err := kube.NewClient(restCfg, "")
	if err != nil {
		return nil, err
	}
	cli, err := internalclientset.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	RegisterTypes()
	kube.EnableCrdWatcher(kubeClient)
	return &client{
		Client:     kubeClient,
		networking: cli,
	}, nil
}

func (c *client) Core() kube.Client {
	return c.Client
}

func (c *client) Networking() internalclientset.Interface {
	return c.networking
}
