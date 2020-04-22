/*
Copyright 2018 The Kubernetes Authors.

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

package main

import (
	"context"
	"os"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	osconfigv1 "github.com/openshift/api/config/v1"
	osclientset "github.com/openshift/client-go/config/clientset/versioned"
	"github.com/openshift/library-go/pkg/config/clusteroperator/v1helpers"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	timeout = time.Second * 10
)

var _ = Describe("Cluster Operator status controller", func() {
	var osClient *osclientset.Clientset
	var statusController *statusController
	var stop chan struct{}

	BeforeEach(func() {
		By("Running the controller")

		var err error
		osClient, err = osclientset.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())

		stop = make(chan struct{})
		statusController = NewStatusController(cfg)
		go func() {
			defer GinkgoRecover()
			statusController.Run(1, stop)
		}()
	})

	AfterEach(func() {
		close(stop)
		os.Unsetenv(releaseVersionEnvVariableName)

		err := osClient.ConfigV1().ClusterOperators().Delete(context.Background(), clusterOperatorName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() error {
			_, err := osClient.ConfigV1().ClusterOperators().Get(context.Background(), clusterOperatorName, metav1.GetOptions{})
			if err != nil && apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}, timeout).Should(Succeed())
	})

	type testCase struct {
		releaseVersionEnvVariableValue string
		// Use a function so that runtime information can be populated (eg UID)
		existingCO *osconfigv1.ClusterOperator
	}

	DescribeTable("should ensure Cluster Operator status is present",
		func(tc testCase) {

			expectedVersion := unknownVersionValue
			if tc.releaseVersionEnvVariableValue != "" {
				expectedVersion = tc.releaseVersionEnvVariableValue

				Expect(os.Setenv(releaseVersionEnvVariableName, tc.releaseVersionEnvVariableValue)).To(Succeed())
				statusController.versionGetter.SetVersion(operatorVersionKey, getReleaseVersion())
			}

			if tc.existingCO != nil {
				_, err := osClient.ConfigV1().ClusterOperators().Create(context.Background(), tc.existingCO, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred())
			}

			Eventually(func() bool {
				co, err := osClient.ConfigV1().ClusterOperators().Get(context.Background(), clusterOperatorName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				// check version.
				if len(co.Status.Versions) != 1 {
					return false
				}
				if co.Status.Versions[0].Name != operatorVersionKey && co.Status.Versions[0].Version != expectedVersion {
					return false
				}

				// check conditions.
				if v1helpers.IsStatusConditionFalse(co.Status.Conditions, osconfigv1.OperatorAvailable) {
					return false
				}
				if v1helpers.IsStatusConditionFalse(co.Status.Conditions, osconfigv1.OperatorUpgradeable) {
					return false
				}
				if v1helpers.IsStatusConditionTrue(co.Status.Conditions, osconfigv1.OperatorDegraded) {
					return false
				}
				if v1helpers.IsStatusConditionTrue(co.Status.Conditions, osconfigv1.OperatorProgressing) {
					return false
				}

				// check related objects.
				if !equality.Semantic.DeepEqual(co.Status.RelatedObjects, relatedObjects) {
					return false
				}
				return true
			}, timeout).Should(BeTrue())
		},
		Entry("when there's no existing cluster operator nor release version", testCase{
			releaseVersionEnvVariableValue: "",
			existingCO:                     nil,
		}),
		Entry("when there's no existing cluster operator but there's release version", testCase{
			releaseVersionEnvVariableValue: "a_cvo_given_version",
			existingCO:                     nil,
		}),
		Entry("when there's an existing cluster operator and a release version", testCase{
			releaseVersionEnvVariableValue: "another_cvo_given_version",
			existingCO: &osconfigv1.ClusterOperator{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterOperatorName,
				},
				Status: osconfigv1.ClusterOperatorStatus{
					Conditions: []osconfigv1.ClusterOperatorStatusCondition{
						{
							Type:               osconfigv1.OperatorAvailable,
							Status:             osconfigv1.ConditionFalse,
							LastTransitionTime: metav1.Now(),
							Reason:             "",
							Message:            "",
						},
						{
							Type:               osconfigv1.OperatorDegraded,
							Status:             osconfigv1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "",
							Message:            "",
						},
						{
							Type:               osconfigv1.OperatorProgressing,
							Status:             osconfigv1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "",
							Message:            "",
						},
						{
							Type:               osconfigv1.OperatorUpgradeable,
							Status:             osconfigv1.ConditionFalse,
							LastTransitionTime: metav1.Now(),
							Reason:             "",
							Message:            "",
						},
					},
					Versions: []osconfigv1.OperandVersion{
						{
							Name:    "anything",
							Version: "anything",
						},
					},
					RelatedObjects: []osconfigv1.ObjectReference{
						{
							Group:    "",
							Resource: "anything",
							Name:     "anything",
						},
					},
				},
			},
		}),
	)
})
