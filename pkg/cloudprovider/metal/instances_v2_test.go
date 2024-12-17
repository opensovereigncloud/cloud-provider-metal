// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metal

import (
	"context"
	"fmt"
	"os"

	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	metalv1alpha1 "github.com/ironcore-dev/metal-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/controller-manager/pkg/clientbuilder"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	"sigs.k8s.io/yaml"
)

var _ = Describe("InstancesV2", func() {
	var (
		instancesProvider cloudprovider.InstancesV2
	)
	ns, cp, clusterName := SetupTest()

	BeforeEach(func() {
		By("Instantiating the instances v2 provider")
		var ok bool
		instancesProvider, ok = (*cp).InstancesV2()
		Expect(ok).To(BeTrue())
	})

	It("Should get instance info", func(ctx SpecContext) {
		By("Creating a Server")
		server := &metalv1alpha1.Server{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
				Labels: map[string]string{
					"instance-type": "foo",
					"zone":          "a",
					"region":        "bar",
				},
			},
			Spec: metalv1alpha1.ServerSpec{
				UUID:  "12345",
				Power: "On",
			},
		}
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		DeferCleanup(k8sClient.Delete, server)

		By("Patching the Server object to have a valid network interface status")
		Eventually(UpdateStatus(server, func() {
			server.Status.PowerState = metalv1alpha1.ServerOnPowerState
			server.Status.NetworkInterfaces = []metalv1alpha1.NetworkInterface{{
				Name: "my-nic",
				IP:   metalv1alpha1.MustParseIP("10.0.0.1"),
			}}
		})).Should(Succeed())

		By("Creating a ServerClaim for a Node")
		serverClaim := &metalv1alpha1.ServerClaim{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "test-",
			},
			Spec: metalv1alpha1.ServerClaimSpec{
				Power:     "On",
				ServerRef: &corev1.LocalObjectReference{Name: server.Name},
			},
		}
		Expect(k8sClient.Create(ctx, serverClaim)).To(Succeed())
		DeferCleanup(k8sClient.Delete, serverClaim)

		By("Creating a Node object with a provider ID referencing the machine")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("Updating the SystemUUID in Node status")
		Eventually(UpdateStatus(node, func() {
			node.Status.NodeInfo.SystemUUID = "12345"
		})).Should(Succeed())

		By("Ensuring that an instance for a Node exists")
		ok, err := instancesProvider.InstanceExists(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		By("Ensuring that the instance is not shut down")
		ok, err = instancesProvider.InstanceShutdown(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())

		By("Ensuring that the instance meta data has the correct addresses")
		instanceMetadata, err := instancesProvider.InstanceMetadata(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Eventually(instanceMetadata).Should(SatisfyAll(
			HaveField("ProviderID", getProviderID(serverClaim.Namespace, serverClaim.Name)),
			HaveField("InstanceType", "foo"),
			HaveField("NodeAddresses", ContainElements(
				corev1.NodeAddress{
					Type:    corev1.NodeInternalIP,
					Address: "10.0.0.1",
				},
			)),
			HaveField("Zone", "a"),
			HaveField("Region", "bar")))

		By("Ensuring cluster name label is added to ServerClaim object")
		Eventually(Object(serverClaim)).Should(SatisfyAll(
			HaveField("Labels", map[string]string{LabelKeyClusterName: clusterName}),
		))
	})

	It("Should get instance info for a Node with correct ProviderID", func(ctx SpecContext) {
		By("Creating a Server")
		server := &metalv1alpha1.Server{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
				Labels: map[string]string{
					"instance-type": "foo",
					"zone":          "a",
					"region":        "bar",
				},
			},
			Spec: metalv1alpha1.ServerSpec{
				UUID:  "12345",
				Power: "On",
			},
		}
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		DeferCleanup(k8sClient.Delete, server)

		By("Patching the Server object to have a valid network interface status")
		Eventually(UpdateStatus(server, func() {
			server.Status.PowerState = metalv1alpha1.ServerOnPowerState
			server.Status.NetworkInterfaces = []metalv1alpha1.NetworkInterface{{
				Name: "my-nic",
				IP:   metalv1alpha1.MustParseIP("10.0.0.1"),
			}}
		})).Should(Succeed())

		By("Creating a ServerClaim for a Node")
		serverClaim := &metalv1alpha1.ServerClaim{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "test-",
			},
			Spec: metalv1alpha1.ServerClaimSpec{
				Power:     "On",
				ServerRef: &corev1.LocalObjectReference{Name: server.Name},
			},
		}
		Expect(k8sClient.Create(ctx, serverClaim)).To(Succeed())
		DeferCleanup(k8sClient.Delete, serverClaim)

		By("Creating a Node object with a provider ID referencing the machine")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
			},
			Spec: corev1.NodeSpec{
				ProviderID: getProviderID(serverClaim.Namespace, serverClaim.Name),
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("Ensuring that an instance for a Node exists")
		ok, err := instancesProvider.InstanceExists(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		By("Ensuring that the instance is not shut down")
		ok, err = instancesProvider.InstanceShutdown(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())

		By("Ensuring that the instance meta data has the correct addresses")
		instanceMetadata, err := instancesProvider.InstanceMetadata(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Eventually(instanceMetadata).Should(SatisfyAll(
			HaveField("ProviderID", getProviderID(serverClaim.Namespace, serverClaim.Name)),
			HaveField("InstanceType", "foo"),
			HaveField("NodeAddresses", ContainElements(
				corev1.NodeAddress{
					Type:    corev1.NodeInternalIP,
					Address: "10.0.0.1",
				},
			)),
			HaveField("Zone", "a"),
			HaveField("Region", "bar")))

		By("Ensuring cluster name label is added to ServerClaim object")
		Eventually(Object(serverClaim)).Should(SatisfyAll(
			HaveField("Labels", map[string]string{LabelKeyClusterName: clusterName}),
		))
	})

	It("Should get InstanceNotFound if no ServerClaim exists for Node", func(ctx SpecContext) {
		By("Creating a Node object with a provider ID referencing non existing ServerClaim")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: corev1.NodeSpec{
				ProviderID: getProviderID(ns.Name, "bar"),
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("Ensuring that an instance for a Node does not exist")
		ok, err := instancesProvider.InstanceExists(ctx, node)
		Expect(err).To(Equal(cloudprovider.InstanceNotFound))
		Expect(ok).To(BeFalse())
	})

	It("Should fail to get instance metadata if no ServerClaim exists for Node", func(ctx SpecContext) {
		By("Creating a node object with a provider ID referencing non existing ServerClaim")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("Ensuring to fail getting the instance metadata")
		metaData, err := instancesProvider.InstanceMetadata(ctx, node)
		Expect(err).To(Equal(cloudprovider.InstanceNotFound))
		Expect(metaData).To(BeNil())
	})

	It("Should fail to get instance shutdown state if no ServerClaim exists for Node", func(ctx SpecContext) {
		By("Creating a Node object with a provider ID referencing non existing ServerClaim")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("ensuring the shutdown state of a node")
		ok, err := instancesProvider.InstanceShutdown(ctx, node)
		Expect(err).To(Equal(cloudprovider.InstanceNotFound))
		Expect(ok).To(BeFalse())
	})

	It("Should not configure node addresses if ConfigureNodeAddresses is false", func(ctx SpecContext) {
		By("Starting up a cloud provider with cloud config to disable node address configuration")
		user, err := testEnv.AddUser(envtest.User{
			Name:   "dummy",
			Groups: []string{"system:authenticated", "system:masters"},
		}, nil)
		Expect(err).NotTo(HaveOccurred())

		kubeconfigData, err := user.KubeConfig()
		Expect(err).NotTo(HaveOccurred())

		clientConfig, err := clientcmd.Load(kubeconfigData)
		Expect(err).NotTo(HaveOccurred())
		clientConfig.Contexts[clientConfig.CurrentContext].Namespace = ns.Name

		namespacedKubeconfigData, err := clientcmd.Write(*clientConfig)
		Expect(err).NotTo(HaveOccurred())

		kubeconfigFile, err := os.CreateTemp(GinkgoT().TempDir(), "kubeconfig")
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = kubeconfigFile.Close()
		}()
		Expect(os.WriteFile(kubeconfigFile.Name(), namespacedKubeconfigData, 0666)).To(Succeed())

		curr := MetalKubeconfigPath
		defer func() {
			MetalKubeconfigPath = curr
		}()
		MetalKubeconfigPath = kubeconfigFile.Name()

		cloudConfigFile, err := os.CreateTemp(GinkgoT().TempDir(), "cloud.yaml")
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = cloudConfigFile.Close()
		}()
		cloudConfig := CloudConfig{
			ClusterName: clusterName,
			Networking: Networking{
				ConfigureNodeAddresses: false,
			},
		}
		cloudConfigData, err := yaml.Marshal(&cloudConfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(cloudConfigFile.Name(), cloudConfigData, 0666)).To(Succeed())

		cloudProviderCtx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		k8sClientSet, err := kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())

		clientBuilder := clientbuilder.NewDynamicClientBuilder(testEnv.Config, k8sClientSet.CoreV1(), ns.Name)
		cp, err := cloudprovider.InitCloudProvider(ProviderName, cloudConfigFile.Name())
		Expect(err).NotTo(HaveOccurred())
		cp.Initialize(clientBuilder, cloudProviderCtx.Done())

		By("Instantiating the instances v2 provider")
		instancesProvider, ok := cp.InstancesV2()
		Expect(ok).To(BeTrue())

		By("Creating a Server")
		server := &metalv1alpha1.Server{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
				Labels: map[string]string{
					"instance-type": "foo",
					"zone":          "a",
					"region":        "bar",
				},
			},
			Spec: metalv1alpha1.ServerSpec{
				UUID:  "12345",
				Power: "On",
			},
		}
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		DeferCleanup(k8sClient.Delete, server)

		By("Patching the Server object to have a valid network interface status")
		Eventually(UpdateStatus(server, func() {
			server.Status.PowerState = metalv1alpha1.ServerOnPowerState
			server.Status.NetworkInterfaces = []metalv1alpha1.NetworkInterface{{
				Name: "my-nic",
				IP:   metalv1alpha1.MustParseIP("10.0.0.1"),
			}}
		})).Should(Succeed())

		By("Creating a ServerClaim for a Node")
		serverClaim := &metalv1alpha1.ServerClaim{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "test-",
			},
			Spec: metalv1alpha1.ServerClaimSpec{
				Power:     "On",
				ServerRef: &corev1.LocalObjectReference{Name: server.Name},
			},
		}
		Expect(k8sClient.Create(ctx, serverClaim)).To(Succeed())
		DeferCleanup(k8sClient.Delete, serverClaim)

		By("Creating a Node object with a provider ID referencing the machine")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
			},
			Spec: corev1.NodeSpec{
				ProviderID: getProviderID(serverClaim.Namespace, serverClaim.Name),
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("Ensuring that an instance for a Node exists")
		ok, err = instancesProvider.InstanceExists(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		By("Ensuring that the instance is not shut down")
		ok, err = instancesProvider.InstanceShutdown(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())

		By("Ensuring that the instance meta data has empty addresses")
		instanceMetadata, err := instancesProvider.InstanceMetadata(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Eventually(instanceMetadata).Should(SatisfyAll(
			HaveField("ProviderID", getProviderID(serverClaim.Namespace, serverClaim.Name)),
			HaveField("InstanceType", "foo"),
			HaveField("NodeAddresses", BeEmpty()),
			HaveField("Zone", "a"),
			HaveField("Region", "bar")))

		By("Ensuring cluster name label is added to ServerClaim object")
		Eventually(Object(serverClaim)).Should(SatisfyAll(
			HaveField("Labels", map[string]string{LabelKeyClusterName: clusterName}),
		))
	})
})

func getProviderID(namespace, serverClaimName string) string {
	return fmt.Sprintf("%s://%s/%s", ProviderName, namespace, serverClaimName)
}
