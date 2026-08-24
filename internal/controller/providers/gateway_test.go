/*
Copyright 2026 The Kubernetes Authors

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

package providers

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func acceptedCondition() metav1.Condition {
	return metav1.Condition{
		Type:               string(gatewayv1.RouteConditionAccepted),
		Status:             metav1.ConditionTrue,
		Reason:             "Accepted",
		LastTransitionTime: metav1.Now(),
	}
}

func TestSchemeFromAcceptedRoute(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatal(err)
	}

	ns := gatewayv1.Namespace("default")

	tests := []struct {
		name        string
		gateway     *gatewayv1.Gateway
		route       *gatewayv1.HTTPRoute
		gwName      string
		gwNamespace string
		want        string
	}{
		{
			name: "https listener",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "default"},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{Name: "https", Protocol: gatewayv1.HTTPSProtocolType, Port: 443},
					},
				},
			},
			route: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Name: "route", Namespace: "default"},
				Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{
					Parents: []gatewayv1.RouteParentStatus{{
						ParentRef:  gatewayv1.ParentReference{Name: "gw", Namespace: &ns},
						Conditions: []metav1.Condition{acceptedCondition()},
					}},
				}},
			},
			gwName: "gw", gwNamespace: "default",
			want: "https",
		},
		{
			name: "http listener",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "default"},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80},
					},
				},
			},
			route: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Name: "route", Namespace: "default"},
				Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{
					Parents: []gatewayv1.RouteParentStatus{{
						ParentRef:  gatewayv1.ParentReference{Name: "gw", Namespace: &ns},
						Conditions: []metav1.Condition{acceptedCondition()},
					}},
				}},
			},
			gwName: "gw", gwNamespace: "default",
			want: "http",
		},
		{
			name: "matches sectionName listener",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "default"},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80},
						{Name: "secure", Protocol: gatewayv1.HTTPSProtocolType, Port: 443},
					},
				},
			},
			route: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Name: "route", Namespace: "default"},
				Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{
					Parents: []gatewayv1.RouteParentStatus{{
						ParentRef: gatewayv1.ParentReference{
							Name:        "gw",
							Namespace:   &ns,
							SectionName: sectionNamePtr("secure"),
						},
						Conditions: []metav1.Condition{acceptedCondition()},
					}},
				}},
			},
			gwName: "gw", gwNamespace: "default",
			want: "https",
		},
		{
			name: "skips non-matching gateway",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{Name: "other-gw", Namespace: "default"},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{Name: "https", Protocol: gatewayv1.HTTPSProtocolType, Port: 443},
					},
				},
			},
			route: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Name: "route", Namespace: "default"},
				Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{
					Parents: []gatewayv1.RouteParentStatus{{
						ParentRef:  gatewayv1.ParentReference{Name: "other-gw", Namespace: &ns},
						Conditions: []metav1.Condition{acceptedCondition()},
					}},
				}},
			},
			gwName: "my-gw", gwNamespace: "default",
			want: "http",
		},
		{
			name: "skips non-matching namespace",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "other-ns"},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{Name: "https", Protocol: gatewayv1.HTTPSProtocolType, Port: 443},
					},
				},
			},
			route: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Name: "route", Namespace: "default"},
				Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{
					Parents: []gatewayv1.RouteParentStatus{{
						ParentRef: gatewayv1.ParentReference{
							Name:      "gw",
							Namespace: namespacePtr("other-ns"),
						},
						Conditions: []metav1.Condition{acceptedCondition()},
					}},
				}},
			},
			gwName: "gw", gwNamespace: "default",
			want: "http",
		},
		{
			name:    "no parents returns http",
			gateway: nil,
			route: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Name: "route", Namespace: "default"},
			},
			gwName: "gw", gwNamespace: "default",
			want: "http",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs := []runtime.Object{}
			if tt.gateway != nil {
				objs = append(objs, tt.gateway)
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			got := SchemeFromAcceptedRoute(context.Background(), c, tt.route, tt.gwName, tt.gwNamespace)
			if got != tt.want {
				t.Errorf("SchemeFromAcceptedRoute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func sectionNamePtr(s string) *gatewayv1.SectionName {
	sn := gatewayv1.SectionName(s)
	return &sn
}

func namespacePtr(s string) *gatewayv1.Namespace {
	ns := gatewayv1.Namespace(s)
	return &ns
}
