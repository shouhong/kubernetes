/*
Copyright 2016 The Kubernetes Authors All rights reserved.

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

package unversioned

import (
	api "k8s.io/kubernetes/pkg/api"
	watch "k8s.io/kubernetes/pkg/watch"
)

// PersistentVolumeNamespacer has methods to work with PersistentVolume resources in a namespace
type PersistentVolumeNamespacer interface {
	PersistentVolumes(namespace string) PersistentVolumeInterface
}

// PersistentVolumeInterface has methods to work with PersistentVolume resources.
type PersistentVolumeInterface interface {
	Create(*api.PersistentVolume) (*api.PersistentVolume, error)
	Update(*api.PersistentVolume) (*api.PersistentVolume, error)
	Delete(name string, options *api.DeleteOptions) error
	DeleteCollection(options *api.DeleteOptions, listOptions api.ListOptions) error
	Get(name string) (*api.PersistentVolume, error)
	List(opts api.ListOptions) (*api.PersistentVolumeList, error)
	Watch(opts api.ListOptions) (watch.Interface, error)
	PersistentVolumeExpansion
}

// persistentVolumes implements PersistentVolumeInterface
type persistentVolumes struct {
	client *LegacyClient
	ns     string
}

// newPersistentVolumes returns a PersistentVolumes
func newPersistentVolumes(c *LegacyClient, namespace string) *persistentVolumes {
	return &persistentVolumes{
		client: c,
		ns:     namespace,
	}
}

// Create takes the representation of a persistentVolume and creates it.  Returns the server's representation of the persistentVolume, and an error, if there is any.
func (c *persistentVolumes) Create(persistentVolume *api.PersistentVolume) (result *api.PersistentVolume, err error) {
	result = &api.PersistentVolume{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("persistentVolumes").
		Body(persistentVolume).
		Do().
		Into(result)
	return
}

// Update takes the representation of a persistentVolume and updates it. Returns the server's representation of the persistentVolume, and an error, if there is any.
func (c *persistentVolumes) Update(persistentVolume *api.PersistentVolume) (result *api.PersistentVolume, err error) {
	result = &api.PersistentVolume{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("persistentVolumes").
		Name(persistentVolume.Name).
		Body(persistentVolume).
		Do().
		Into(result)
	return
}

// Delete takes name of the persistentVolume and deletes it. Returns an error if one occurs.
func (c *persistentVolumes) Delete(name string, options *api.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("persistentVolumes").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *persistentVolumes) DeleteCollection(options *api.DeleteOptions, listOptions api.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("persistentVolumes").
		VersionedParams(&listOptions, api.Scheme).
		Body(options).
		Do().
		Error()
}

// Get takes name of the persistentVolume, and returns the corresponding persistentVolume object, and an error if there is any.
func (c *persistentVolumes) Get(name string) (result *api.PersistentVolume, err error) {
	result = &api.PersistentVolume{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("persistentVolumes").
		Name(name).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of PersistentVolumes that match those selectors.
func (c *persistentVolumes) List(opts api.ListOptions) (result *api.PersistentVolumeList, err error) {
	result = &api.PersistentVolumeList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("persistentVolumes").
		VersionedParams(&opts, api.Scheme).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested persistentVolumes.
func (c *persistentVolumes) Watch(opts api.ListOptions) (watch.Interface, error) {
	return c.client.Get().
		Prefix("watch").
		Namespace(c.ns).
		Resource("persistentVolumes").
		VersionedParams(&opts, api.Scheme).
		Watch()
}
