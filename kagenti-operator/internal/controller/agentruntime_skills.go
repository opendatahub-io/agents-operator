/*
Copyright 2026.

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

package controller

import (
	"strings"

	corev1 "k8s.io/api/core/v1"

	agentv1alpha1 "github.com/kagenti/operator/api/v1alpha1"
)

const (
	SkillVolumePrefix = "skill-"

	ConditionTypeSkillsMounted = "SkillsMounted"
)

// reconcileSkillVolumes declaratively reconciles skill ImageVolumes in a PodSpec.
// It adds volumes/mounts for desired skills and removes any stale skill-prefixed
// volumes no longer in the desired list. Pass nil to remove all skill volumes.
func reconcileSkillVolumes(podSpec *corev1.PodSpec, desiredSkills []agentv1alpha1.SkillImageRef) {
	desired := make(map[string]agentv1alpha1.SkillImageRef, len(desiredSkills))
	for _, s := range desiredSkills {
		desired[SkillVolumePrefix+s.Name] = s
	}

	var keptVolumes []corev1.Volume
	for _, v := range podSpec.Volumes {
		if !strings.HasPrefix(v.Name, SkillVolumePrefix) {
			keptVolumes = append(keptVolumes, v)
			continue
		}
		if skill, ok := desired[v.Name]; ok {
			keptVolumes = append(keptVolumes, buildSkillVolume(skill))
			delete(desired, v.Name)
		}
	}
	for _, skill := range desiredSkills {
		volName := SkillVolumePrefix + skill.Name
		if _, ok := desired[volName]; ok {
			keptVolumes = append(keptVolumes, buildSkillVolume(skill))
		}
	}
	podSpec.Volumes = keptVolumes

	desiredMountNames := make(map[string]bool, len(desiredSkills))
	for _, s := range desiredSkills {
		desiredMountNames[SkillVolumePrefix+s.Name] = true
	}
	if len(podSpec.Containers) > 0 {
		reconcileSkillMounts(&podSpec.Containers[0], desiredSkills, desiredMountNames)
	}
}

func buildSkillVolume(skill agentv1alpha1.SkillImageRef) corev1.Volume {
	return corev1.Volume{
		Name: SkillVolumePrefix + skill.Name,
		VolumeSource: corev1.VolumeSource{
			Image: &corev1.ImageVolumeSource{
				Reference:  skill.Image,
				PullPolicy: corev1.PullPolicy(skill.PullPolicy),
			},
		},
	}
}

func reconcileSkillMounts(container *corev1.Container, desiredSkills []agentv1alpha1.SkillImageRef, desiredMountNames map[string]bool) {
	var keptMounts []corev1.VolumeMount
	for _, m := range container.VolumeMounts {
		if strings.HasPrefix(m.Name, SkillVolumePrefix) && !desiredMountNames[m.Name] {
			continue
		}
		if !strings.HasPrefix(m.Name, SkillVolumePrefix) {
			keptMounts = append(keptMounts, m)
		}
	}
	for _, s := range desiredSkills {
		keptMounts = append(keptMounts, corev1.VolumeMount{
			Name:      SkillVolumePrefix + s.Name,
			MountPath: s.MountPath,
			ReadOnly:  true,
		})
	}
	container.VolumeMounts = keptMounts
}
