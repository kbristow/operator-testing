# Testing with Operators
This document describes some methods used for testing with operators. To get started with this, the source code can be retrieved using:

```shell=
git clone --branch initial-state https://github.com/kbristow/operator-testing.git
```

This is an operator that is specifically designed to run along side a local api (simple-api) and will create config maps based on json files that can be retrieved from the api. To run the operator do the following:
- Start the simple-api (see the README for instructions)
- Get a local `k3d` cluster running and make sure your local kubeconfig has it as the current context
- Run `export CONFIG_URL=http://127.0.0.1:8000 && make install run`
- Create a sample MyConfig `kubectl apply -f config/samples/simple_v1alpha1_myconfig.yaml`
- See how it reconciles and creates a config map :tada:

## Testing
Now that we have a running operator, lets look at how to test this operator.

### Testing overview
Testing in operators is done using a framework called [Ginkgo](https://github.com/onsi/ginkgo). Ginkgo provides mechanisms for testing asynchronous results such as expecting something to eventually reach a specified state. This aligns really well with the Kubernetes model of declare your desired state and let K8's do its magic to make it eventually so.

The tests we write execute against a local instance of the kube-api-server. So we write tests that interact with the kube-api, which our controllers monitor and act accordingly to the changes our tests are making (creating a new manifest, updating a manifest, deleting a manifest etc). We can then assert that the controllers perform the steps we expect them to.

### The MyConfig reconcile loop
Before defining some tests, lets start by understanding how the simple-operator executes. Once we know this, we can start identifying what needs testing. Lets start by looking at a CR definition:

```yaml=
apiVersion: simple.absa.subatomic/v1alpha1
kind: MyConfig
metadata:
  name: myconfig-sample
spec:
  configName: example-1
  targetConfigMap: some-config-map
```

Using the above definition, the following logic is applied in the reconcile loop:
- Talk to the simple-api and retrieve the config by the name of `example-1` (`config`)
- Look for a config map named `some-config-map` in the same namespace as `myconfig-sample`
    - If `some-config-map` does not exists, create a config map called `some-config-map` and store `config` as the value against a key `example-1`
    - If `some-config-map` does exist, check if the value of the key `example-1` matches the value of `config`, and if not, update the config-map.
- If the config map was created or updated, update the status `version` property to be incremented by 1 (the initial "empty" value is 0 so on create this will set the version to 1)

### Laying out testing scenarios
Using the above understanding, we can layout a few simple testing scenarios:

1. Create a MyConfig instance
    - expect that an api call gets made and the config requested exists
    - expect that some-config-map gets created with the correct details
    - expect that version number in the status gets set to 1
2. Update the MyConfig instance with a new `configName`
    - expect that an api call gets made and the config requested exists
    - expect that some-config-map gets updated with the correct details
    - expect that version number in the status gets set to 2
3. Update the config behind `simple-api`
    - expect that an api call gets made and the config requested exists
    - expect that some-config-map gets updated with the correct details
    - expect that version number in the status gets set to 3

These are a few scenarios out of many possible ones but they should allow us to discuss some different approaches to testing.

### General Setup
Before we can write tests for the controller, we have to modify `suite_test.go` to start the controller and setup some config around how the k8's client is managed. This process is explained in detail on the kubebuilder [instructions](https://book.kubebuilder.io/cronjob-tutorial/writing-tests.html) for writing tests. The gist of it is we need to add some registration logic below the point in `suite_test.go` where the k8sclient is defined so that we have the following:

```go=
k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
Expect(err).NotTo(HaveOccurred())
Expect(k8sClient).NotTo(BeNil())

// We need to add everything below this

k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
    Scheme:             scheme.Scheme,
    MetricsBindAddress: "127.0.0.1:8888",
})
Expect(err).ToNot(HaveOccurred())

err = (&MyConfigReconciler{
    Client: k8sClient,
    Scheme: k8sManager.GetScheme(),
    Log:    ctrl.Log.WithName("controllers").WithName("MyConfig"),
}).SetupWithManager(k8sManager)
Expect(err).ToNot(HaveOccurred())

go func() {
    err = k8sManager.Start(ctrl.SetupSignalHandler())
    Expect(err).ToNot(HaveOccurred())
}()
```

### Testing with mocks
We have an external service that the controller interacts with to perform it's reconcile. We want to stub out the reponses from this server so that the test can run without having to have an instance of simple-api running. One option for this is using mocks to mock out either the httpClient making the calls, or the service interface for `simple-api`. In this scenario we are going to mock out the service interface because it is easier due to how we are going to write our tests. The service itself can then be unit tested directly using httpClient mocking. For examples on how to mock the httpClient, see this [article](https://levelup.gitconnected.com/mocking-outbound-http-calls-in-golang-9e5a044c2555).

#### Setup

In order to employ this mocking strategy, we need to code to an interface when creating the service to interact with the `simple-api`. This is already done and is implemented in the `config_api` package. The below excerpt shows this:

```go=
type ConfigService interface {
	GetConfig(string) (string, error)
}

type Service struct {
	Url string
}

func newService() ConfigService {
	url := os.Getenv("CONFIG_URL")
	return Service{Url: url}
}

var ConfigServiceFactoryFunction = newService

func NewConfigService() ConfigService {
	return ConfigServiceFactoryFunction()
}

func (s Service) GetConfig(configName string) (string, error) {
	// implementation is here
}
```

We define the interface `ConfigService` which has the only function we need: `GetConfig`. We then create the `Service` struct which we implement the interface with by creating the `func (s Service) GetConfig(configName string) (string, error)` function. Additionally we need to provide a way to inject another implementation for when we mock this service and we do this using the swappable factory implementation. The `NewConfigService` calls `ConfigServiceFactoryFunction` which can be swapped to a different implementation but by default calls `newService` which creates our Service instance.

#### Mocks
We can now create mocks for this interface. We create mocks using the [mockery framework](https://github.com/vektra/mockery) which can be installed using:
```shell=
brew install mockery
brew upgrade mockery
```
The mocks for the `ConfigService` can then be generated with
```shell=
mockery --all --keeptree
```
which generates mocks for any interfaces found, and keeps the folder structure the same but within a `./mocks` directory. These mocks are generated code, and no manual editting should ever be required. If you update your interface, then you should re-generate the mocks.

These mocks provide mocks that can be used as is, but we will provide a wrapper layer that allows us to extend the mocking framework if we wish. Create the wrapper layer in `suite_test.go` with no additional functionality yet, but we will extend it later to demonstrate the pattern. Add the following wrapper type:

```go=
type configServiceMockWrapper struct {
	mocks.ConfigService
}
```

#### Writing a test
Now that we have the framework setup to mock our service lets try write a test.

Create a `myconfig_controller_test.go` file to contain the tests for the controller. We can setup some base scenario boilerplay as follows:

```go=
package controllers

import (
	"context"
	simplev1alpha1 "github.com/kbristow/simple-operator/api/v1alpha1"
	configsdk "github.com/kbristow/simple-operator/config-sdk"
	mocks "github.com/kbristow/simple-operator/mocks/config-sdk"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		// We will write our tests here
	})
})

```
The consts at the top are our general values for how often and for how long we are going to wait for conditions for tests to be satisfied. The `Context` is the syntax used by Ginkgo to define your tests. We will use additional syntax in our test definitions such as `It`, `Should` and `By` which are also part of the Ginkgo way of structuring tests and are additionally used to describe in a human readable format what the test is doing.

Inside the context we can start by defining some data that will be used within the test:

```go=
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
```
This includes constants for the name and namespace of our resource, the initial resource we want to create, and some lookup keys for the config map we expect to be generated and the MyConfig itself.

After defining the data we are going to use for the test, we can create a simple base for ours test as below

```go=

It("Should correctly manage the MyConfig resource", func() {
    // Create a mock ConfigService
    configServiceMock := &configServiceMockWrapper{
        ConfigService: mocks.ConfigService{},
    }
    // Modify the ConfigServiceFactoryFunction to return our mock as the ConfigService instance
    configsdk.ConfigServiceFactoryFunction = func() configsdk.ConfigService {
        return configServiceMock
    }

    // Create a mock that return a json value and mark a flag that indicates that the mock was called
    getExample1ConfigCalled := false
    example1ConfigReturnValue := "{\"key\":\"value\"}"
    configServiceMock.On("GetConfig", "example-1").
        Return(example1ConfigReturnValue, nil).
        Run(func(args mock.Arguments) {
            getExample1ConfigCalled = true
        })

    // Initial a context (always need one)
    ctx := context.Background()

    By("By creating a new MyConfig resource")
    Expect(k8sClient.Create(ctx, myConfig)).Should(Succeed())

    By("By making a call to get the example-1 config")
    Eventually(func() bool {
        return getExample1ConfigCalled
    }, timeout, interval).Should(Equal(true))
})
```

As per the comments we set up our mock to be injected into anything using the factory function. We then configure the mock to say, when `GetConfig` is called, with `example-1` as the parameter (as per the MyConfig definition from above), return the specified json string and no error, then run a function to change `getExample1ConfigCalled` to `true` to indicate the mock was called. You could also make use of `configServiceMock.assertCalled` but I find it does not fit with the Ginkgo frame work so niceley.

We then create an instance of our MyConfig and expect it to succeed. Then we know that the controller should start a reconcile and in doing so it should make a call to `simple-api` and our mocked function should get invoked. Since this is happening in the background (not synchronously within our test context) we use an `Eventually` statement so say that we expect `getExample1ConfigCalled` to get set to `true`. We check it's value every `interval` time period, and fail the check if we wait longer than `timeout` amount of time for the condition to be met.

The test should now be runnable using the command below and they should pass
```shell=
make test
```

The next step in the initial test we defined earlier can be tested by adding a check to see that the config map expected get created correctly.

```go=
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
}, timeout, interval).Should(Equal(example1ConfigReturnValue))
```

Here we use the k8s client to try get the expected config map using the lookup key defined before. Return a failure result if we cannot find the config map, otherwise get the value for the "example-1" key and return it. We expect the value returned from this testing function to eventaully equal the `example1ConfigReturnValue` defined earlier.

The tests can be run again and should pass.

The initial testing scenario can now be rounded out by adding a check to assert that the status version property gets updated.

```go=
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

```

Much like the previous test, we get the current state of `my-config` from the cluster and return the version stored in it's `Status.Version` property. We expect that the version should eventually equal 1.

The tests can be run again and should pass.

Moving onto test scenario 2, we now want to change the definition of this MyConfig so that it pulls a config by a different name and then updates the config map appropriately. Lets start by adding a new mock. At the top of the test context where we previously set up our mocks add the new mock in the same way that we previously added a mock.

```go=
// Create a mock that return a json value and mark a flag that indicates that the mock was called
getExample2ConfigCalled := false
example2ConfigReturnValue := "{\"key2\":\"value2\"}"
configServiceMock.On("GetConfig", "example-2").
    Return(example2ConfigReturnValue, nil).
    Run(func(args mock.Arguments) {
        getExample2ConfigCalled = true
    })
```

Next we can update the `myConfig` definition to use the `example-2` config and push this change to the kube-api. We then expect the new mock to get called. Add the following below the previous test

```go=
By("Updating the my-config definition to use the example-2 config")
myConfig.Spec.ConfigName = "example-2"
Expect(k8sClient.Update(ctx, myConfig)).Should(Succeed())

By("By making a call to get the example-2 config")
Eventually(func() bool {
    return getExample2ConfigCalled
}, timeout, interval).Should(Equal(true))
```

The tests can be run again and should pass.

The remainder of our test scenario can be implemented the same way as our previous tests. The below should be familiar and can be added below that last test

```go=
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
```

The final testing scenario is a somewhat fabricated example of a test in order to demonstrate a pattern that is possible to run into when writing tests for operators that interact with external services. What this test is trying to demonstrate is how you can mock out an api that *returns different values for identical call depending on what has previously happened in your test*.

An example of where this is useful is lets say you have an operator that maintins an entity behind a rest api defined by a CRD instance. When you create you CRD instance, it checks if the entity exists (`GET /entity/my-entity`), and then creates it with the specified definition because it does not exist. You then update the CRD instance and the operator reconcile runs again, and this time the check if it exists (`GET /entity/my-entity`) returns true even though it was the exact same call. The operator now updates the entity in the rest api instead of creating a new entity. That exact same call with different responses cannot be implemented using the `On` syntax with mockery mock objects. The alternative pattern we will look at can be used to do this kind of mocking, as well as could be used for complex logic based mocking

Lets copy the first test scenario again to pull the `example-1` config but change the mock to have it return another value. Lets start by going back to the mock wrapper and change it to the following

```go=
type configServiceMockWrapper struct {
	mocks.ConfigService
	getConfigOverwriteFn func(string)(string, error)
}

// Replace GetConfig with a function that calls getConfigOverwriteFn override function
// if it is defined, otherwise call the underlying mock object
func (service *configServiceMockWrapper) GetConfig(name string) (string, error)  {
	if service.getConfigOverwriteFn != nil {
		return service.getConfigOverwriteFn(name)
	}else {
		return service.ConfigService.GetConfig(name)
	}
}
```

This gives us a way to extend the mock so that we can overwrite a function with more complex logic or if we do not need that, we can still use the underlying mock for `On` style mocks if we require.

Lets rewrite our previous `On` mocks using this pattern

```go=
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
```

This creates an overwrite function for GetConfig that covers all 3 of our mock cases for this test. Again this is a fabricated situation so it may seem silly to do this but it is meant to convey the pattern for when it is actually useful to use. We then need to update our scenario 1 tests so that they use the new variables defined above.

```go=
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
```

We can run the tests again to check that our tests still pass.

Now we can add a third set of tests to test the final scenario very similar to the original set of tests.

```go=
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
```

We can rerun the tests again to see they still pass.


### HttpTest Server
Instead of mocking out the service, we could instead create a fake server that will respond with the responses as we require. Go provides a package that allows us to do this out of the box called http test. Lets add a new test context that implements scenario 1 using this strategy.

In order for the next context not to be contaminated by the first context, we must delete the original MyConfig instance. Add the following to the end of the last test

```go=
// Delete to not interfere with next test
_ = k8sClient.Delete(ctx, myConfig)
```

Set up the next context with the same data used for the previous tests
```go=
Context("Using http server mocks, when updating a MyConfig", func() {
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
        // Write test here
    })
})
```

Create the fake server

```go=
example1ConfigReturnValue := "{\"key\":\"value\"}"
example1ConfigCalled := false

ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.URL.Path == "/example-1" {
        // implement fake response for /example-1 route
        example1ConfigCalled = true
        w.Write([]byte(example1ConfigReturnValue))
        w.WriteHeader(200)
    } else {
        // Fail on any unexpected route call
        w.WriteHeader(500)
        w.Write([]byte("Unkown route"))
    }
}))
// end server when exiting scope
defer ts.Close()
```
Create the fake server, and implement a response similar to how we mocked the Service responses earlier. We have to implement the full http response logic to have this work correctly.

We can now swap the factory function with one that creates a real service pointing to our fake server.

```go=
// Modify the ConfigServiceFactoryFunction to return Service point at fake server url
configsdk.ConfigServiceFactoryFunction = func() configsdk.ConfigService {
    return configsdk.Service{
        Url: ts.URL,
    }
}
```
Then we can define the test the same way as in the original scenario

```go=
// Initial a context (always need one)
ctx := context.Background()

By("By creating a new MyConfig resource")
Expect(k8sClient.Create(ctx, myConfig)).Should(Succeed())

By("By making a call to get the example-1 config")
Eventually(func() bool {
    return example1ConfigCalled
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
}, timeout, interval).Should(Equal(example1ConfigReturnValue))

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
```

We can run our tests and see that they still pass.

It should be obvious that this method can be used to implement the same logic of multiple calls to the same url at different points in the test to return different results as using the mock alternate pattern fairly simply.


### Pros and cons
The mock approach is useful in situation where there are many calls you need to mock out and most of them would be suited using the `On` style of mocking but with a few occassions (or none at all) of requiring the extra power the function override ability. This is also very useful if you have many tests use the same endpoint calls because there is less overhead per mock using this method than the httptest method.

However, there is a lot more boilerplate required to set up the initial mocks, wrapper and override functions. Mocks must also be regenerated every time the interface changes, and we are in a situation that we would also need to write unit tests for the service itself. The whole mocking approach is also more complex to grasp than the httptest approach.

The httptest approach is useful if most responses you have to fake require complex logic and you do not have many similar calls between the tests. You would in these cases not benefit too much from the reduced boilerplate the `On` style mocking provides. The httptest model is also fairly easy to grasp as it is all just defined in a single place without any mock frameworks and indirection required. You additionally will be testing the entire code base instead of skipping the service layer.

The downside to the httptest approach is that there is a lot of boilerplate code per test. This may not be obvious from the example shown, but depending on the api your are faking responses for, you may have to deal with headers, body marshalling/unmarshalling, and various response codes for multiple endpoints and sometimes the same ones repetitively. This can result in a lot of extra code in your test base that would need maintaining.