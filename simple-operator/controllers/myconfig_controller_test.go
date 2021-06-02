package controllers

import (
	"context"
	"fmt"
	simplev1alpha1 "github.com/kbristow/simple-operator/api/v1alpha1"
	configsdk "github.com/kbristow/simple-operator/config-sdk"
	mocks "github.com/kbristow/simple-operator/mocks/config-sdk"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"time"
)

var _ = Describe("MyConfig controller", func() {
	// Define utility constants for testing timeouts/durations and intervals.
	const (
		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When updating a MyConfig", func() {
		// Define utility constants for object names for this test
		const (
			MyConfigName      = "test-myconfig"
			MyConfigNamespace = "default"
		)

		var (
			myConfig = &simplev1alpha1.MyConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "simple.absa.subatomic/v1alpha1",
					Kind:       "MyConfig",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      MyConfigName,
					Namespace: MyConfigNamespace,
				},
				Spec: simplev1alpha1.MyConfigSpec{
					ConfigName:      "example-1",
					TargetConfigMap: "a-config-map",
				},
			}
			myConfigLookupKey = types.NamespacedName{
				Namespace: myConfig.Namespace,
				Name:      MyConfigName,
			}
			aConfigMapLookupKey = types.NamespacedName{
				Namespace: myConfig.Namespace,
				Name:      "a-config-map",
			}
		)
		It("Should correctly manage the MyConfig resource", func() {
			// Create a mock ConfigService
			configServiceMock := &configServiceMockWrapper{
				ConfigService: mocks.ConfigService{},
			}
			// Modify the ConfigServiceFactoryFunction to return our mock as the ConfigService instance
			configsdk.ConfigServiceFactoryFunction = func() configsdk.ConfigService {
				return configServiceMock
			}

			example1ConfigFirstCalled := false
			example1ConfigSecondCalled := false
			example1ConfigScenario := "1"
			example1ConfigReturnValueFirst := "{\"key\":\"value\"}"
			example1ConfigReturnValueSecond := "{\"key\":\"value2\"}"

			getExample2ConfigCalled := false
			example2ConfigReturnValue := "{\"key2\":\"value2\"}"

			configServiceMock.getConfigOverwriteFn = func(configName string) (string, error) {
				if configName == "example-1" {
					// Return example1ConfigReturnValueFirst for example1ConfigScenario 1
					// Return example1ConfigReturnValueSecond for example1ConfigScenario 2
					if example1ConfigScenario == "1" {
						example1ConfigFirstCalled = true
						return example1ConfigReturnValueFirst, nil
					} else if example1ConfigScenario == "2" {
						example1ConfigSecondCalled = true
						return example1ConfigReturnValueSecond, nil
					}
				} else if configName == "example-2" {
					// Return example2ConfigReturnValue and record that GetConfig was called with
					// getExample2ConfigCalled
					getExample2ConfigCalled = true
					return example2ConfigReturnValue, nil
				}
				// Return error on unexpected call
				return "", fmt.Errorf("unexpected config name: %s", configName)
			}

			// Initial a context (always need one)
			ctx := context.Background()

			By("By creating a new MyConfig resource")
			Expect(k8sClient.Create(ctx, myConfig)).Should(Succeed())

			By("By making a call to get the example-1 config")
			Eventually(func() bool {
				return example1ConfigFirstCalled
			}, timeout, interval).Should(Equal(true))

			By("Creating a-config-map with the correct data")
			Eventually(func() (string, error) {
				// Try get the config map expected
				var aConfigMap corev1.ConfigMap
				err := k8sClient.Get(ctx, aConfigMapLookupKey, &aConfigMap)
				if err != nil {
					// Fail if not successfully found
					return "", err
				}

				// Get the data for the example-1 key
				example1Config := aConfigMap.Data["example-1"]

				// And return it
				return example1Config, nil
			}, timeout, interval).Should(Equal(example1ConfigReturnValueFirst))

			By("Creating setting my-config's status version property to 1")
			Eventually(func() (int, error) {
				// Get the updated state of myConfig
				err := k8sClient.Get(ctx, myConfigLookupKey, myConfig)
				if err != nil {
					// Fail if not successfully retrieved
					return 0, err
				}

				// Return the current version
				return myConfig.Status.Version, nil
			}, timeout, interval).Should(Equal(1))

			By("Updating the my-config definition to use the example-2 config")
			myConfig.Spec.ConfigName = "example-2"
			Expect(k8sClient.Update(ctx, myConfig)).Should(Succeed())

			By("By making a call to get the example-2 config")
			Eventually(func() bool {
				return getExample2ConfigCalled
			}, timeout, interval).Should(Equal(true))

			By("Updating a-config-map with the correct data")
			Eventually(func() (string, error) {
				// Try get the config map expected
				var aConfigMap corev1.ConfigMap
				err := k8sClient.Get(ctx, aConfigMapLookupKey, &aConfigMap)
				if err != nil {
					// Fail if not successfully found
					return "", err
				}

				// Get the data for the example-1 key
				example1Config := aConfigMap.Data["example-2"]

				// And return it
				return example1Config, nil
			}, timeout, interval).Should(Equal(example2ConfigReturnValue))

			By("Creating setting my-config's status version property to 2")
			Eventually(func() (int, error) {
				// Get the updated state of myConfig
				err := k8sClient.Get(ctx, myConfigLookupKey, myConfig)
				if err != nil {
					// Fail if not successfully retrieved
					return 0, err
				}

				// Return the current version
				return myConfig.Status.Version, nil
			}, timeout, interval).Should(Equal(2))

			// Switch to the second example return scenario
			example1ConfigScenario = "2"

			By("Updating the my-config definition to use the example-1 config again")
			myConfig.Spec.ConfigName = "example-1"
			Expect(k8sClient.Update(ctx, myConfig)).Should(Succeed())

			By("By making a call to get the example-1 config")
			Eventually(func() bool {
				return example1ConfigSecondCalled
			}, timeout, interval).Should(Equal(true))

			By("Creating a-config-map with the correct data")
			Eventually(func() (string, error) {
				// Try get the config map expected
				var aConfigMap corev1.ConfigMap
				err := k8sClient.Get(ctx, aConfigMapLookupKey, &aConfigMap)
				if err != nil {
					// Fail if not successfully found
					return "", err
				}

				// Get the data for the example-1 key
				example1Config := aConfigMap.Data["example-1"]

				// And return it
				return example1Config, nil
			}, timeout, interval).Should(Equal(example1ConfigReturnValueSecond))

			By("Creating setting my-config's status version property to 3")
			Eventually(func() (int, error) {
				// Get the updated state of myConfig
				err := k8sClient.Get(ctx, myConfigLookupKey, myConfig)
				if err != nil {
					// Fail if not successfully retrieved
					return 0, err
				}

				// Return the current version
				return myConfig.Status.Version, nil
			}, timeout, interval).Should(Equal(3))
		})
	})
})
