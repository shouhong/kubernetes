/*
Copyright 2015 The Kubernetes Authors All rights reserved.

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

package v1alpha1

import (
	"time"

	"k8s.io/kubernetes/pkg/api/unversioned"
	"k8s.io/kubernetes/pkg/kubelet/qos"
	"k8s.io/kubernetes/pkg/runtime"
)

func addDefaultingFuncs(scheme *runtime.Scheme) {
	scheme.AddDefaultingFuncs(
		func(obj *KubeProxyConfiguration) {
			if obj.BindAddress == "" {
				obj.BindAddress = "0.0.0.0"
			}
			if obj.HealthzPort == 0 {
				obj.HealthzPort = 10249
			}
			if obj.HealthzBindAddress == "" {
				obj.HealthzBindAddress = "127.0.0.1"
			}
			if obj.OOMScoreAdj == nil {
				temp := int32(qos.KubeProxyOOMScoreAdj)
				obj.OOMScoreAdj = &temp
			}
			if obj.ResourceContainer == "" {
				obj.ResourceContainer = "/kube-proxy"
			}
			if obj.IPTablesSyncPeriod.Duration == 0 {
				obj.IPTablesSyncPeriod = unversioned.Duration{30 * time.Second}
			}
			zero := unversioned.Duration{}
			if obj.UDPIdleTimeout == zero {
				obj.UDPIdleTimeout = unversioned.Duration{250 * time.Millisecond}
			}
			if obj.ConntrackMax == 0 {
				obj.ConntrackMax = 256 * 1024 // 4x default (64k)
			}
			if obj.ConntrackTCPEstablishedTimeout == zero {
				obj.ConntrackTCPEstablishedTimeout = unversioned.Duration{Duration: 24 * time.Hour} // 1 day (1/5 default)
			}
		},
	)
}
