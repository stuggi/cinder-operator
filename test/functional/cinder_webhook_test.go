/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package functional

import (
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2" //revive:disable:dot-imports
	. "github.com/onsi/gomega"    //revive:disable:dot-imports

	k8s_errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func cinderWithServicePortOverride(name, endpoint, portName string) *unstructured.Unstructured {
	spec := GetDefaultCinderSpec()
	cinderAPISpec := spec["cinderAPI"].(map[string]any)
	cinderAPISpec["override"] = map[string]any{
		"service": map[string]any{
			endpoint: map[string]any{
				"spec": map[string]any{
					"ports": []any{
						map[string]any{"name": portName, "port": int64(8776)},
					},
				},
			},
		},
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cinder.openstack.org/v1beta1",
		"kind":       "Cinder",
		"metadata": map[string]any{
			"name":      name,
			"namespace": cinderName.Namespace,
		},
		"spec": spec,
	}}
}

func setCinderServicePortName(obj *unstructured.Unstructured, endpoint, portName string) {
	spec := obj.Object["spec"].(map[string]any)
	cinderAPISpec := spec["cinderAPI"].(map[string]any)
	override := cinderAPISpec["override"].(map[string]any)
	serviceOverrides := override["service"].(map[string]any)
	serviceOverride := serviceOverrides[endpoint].(map[string]any)
	serviceSpec := serviceOverride["spec"].(map[string]any)
	ports := serviceSpec["ports"].([]any)
	ports[0].(map[string]any)["name"] = portName
}

var _ = Describe("Cinder webhook", func() {
	It("validates public and internal service port names on create and update", func() {
		for _, endpoint := range []string{"public", "internal"} {
			validName := fmt.Sprintf("%s-%s-port-override", cinderName.Name, endpoint)
			valid := cinderWithServicePortOverride(validName, endpoint, endpoint)
			_, err := controllerutil.CreateOrPatch(
				ctx, k8sClient, valid, func() error { return nil })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, valid)
			})

			invalidCreate := cinderWithServicePortOverride(validName+"-invalid", endpoint, "wrong")
			_, err = controllerutil.CreateOrPatch(
				ctx, k8sClient, invalidCreate, func() error { return nil })
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("must be %q", endpoint)))

			invalidUpdate := cinderWithServicePortOverride(validName, endpoint, endpoint)
			Eventually(func(g Gomega) {
				_, err := controllerutil.CreateOrPatch(ctx, k8sClient, invalidUpdate, func() error {
					setCinderServicePortName(invalidUpdate, endpoint, "wrong")
					return nil
				})
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("must be %q", endpoint)))
			}, timeout, interval).Should(Succeed())
		}
	})

	It("rejects update to deprecated rabbitMqClusterName field", func() {
		spec := GetDefaultCinderSpec()
		spec["rabbitMqClusterName"] = "rabbitmq"

		raw := map[string]any{
			"apiVersion": "cinder.openstack.org/v1beta1",
			"kind":       "Cinder",
			"metadata": map[string]any{
				"name":      cinderName.Name,
				"namespace": cinderName.Namespace,
			},
			"spec": spec,
		}

		// Create the Cinder instance
		unstructuredObj := &unstructured.Unstructured{Object: raw}
		_, err := controllerutil.CreateOrPatch(
			ctx, k8sClient, unstructuredObj, func() error { return nil })
		Expect(err).ShouldNot(HaveOccurred())

		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, unstructuredObj)
		})

		// Try to update rabbitMqClusterName
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, cinderName, unstructuredObj)).Should(Succeed())
			specMap := unstructuredObj.Object["spec"].(map[string]any)
			specMap["rabbitMqClusterName"] = "rabbitmq2"
			err := k8sClient.Update(ctx, unstructuredObj)
			g.Expect(err).Should(HaveOccurred())

			var statusError *k8s_errors.StatusError
			g.Expect(errors.As(err, &statusError)).To(BeTrue())
			g.Expect(statusError.ErrStatus.Details.Kind).To(Equal("Cinder"))
			g.Expect(statusError.ErrStatus.Message).To(
				ContainSubstring("field \"spec.rabbitMqClusterName\" is deprecated"))
			g.Expect(statusError.ErrStatus.Message).To(
				ContainSubstring("use \"spec.messagingBus.cluster\" instead"))
		}, timeout, interval).Should(Succeed())
	})

	It("rejects update to deprecated notificationsBusInstance field", func() {
		spec := GetDefaultCinderSpec()
		notificationsBusInstance := "notifications-rabbitmq"
		spec["notificationsBusInstance"] = notificationsBusInstance

		cinderName2 := cinderName
		cinderName2.Name = cinderName.Name + "-notifications"

		raw := map[string]any{
			"apiVersion": "cinder.openstack.org/v1beta1",
			"kind":       "Cinder",
			"metadata": map[string]any{
				"name":      cinderName2.Name,
				"namespace": cinderName2.Namespace,
			},
			"spec": spec,
		}

		// Create the Cinder instance
		unstructuredObj := &unstructured.Unstructured{Object: raw}
		_, err := controllerutil.CreateOrPatch(
			ctx, k8sClient, unstructuredObj, func() error { return nil })
		Expect(err).ShouldNot(HaveOccurred())

		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, unstructuredObj)
		})

		// Try to update notificationsBusInstance
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, cinderName2, unstructuredObj)).Should(Succeed())
			specMap := unstructuredObj.Object["spec"].(map[string]any)
			specMap["notificationsBusInstance"] = "notifications-rabbitmq2"
			err := k8sClient.Update(ctx, unstructuredObj)
			g.Expect(err).Should(HaveOccurred())

			var statusError *k8s_errors.StatusError
			g.Expect(errors.As(err, &statusError)).To(BeTrue())
			g.Expect(statusError.ErrStatus.Details.Kind).To(Equal("Cinder"))
			g.Expect(statusError.ErrStatus.Message).To(
				ContainSubstring("field \"spec.notificationsBusInstance\" is deprecated"))
			g.Expect(statusError.ErrStatus.Message).To(
				ContainSubstring("use \"spec.notificationsBus.cluster\" instead"))
		}, timeout, interval).Should(Succeed())
	})
})
