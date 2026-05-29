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
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	agentv1alpha1 "github.com/kagenti/operator/api/v1alpha1"
)

var _ = Describe("Skill Volume Reconciliation", func() {
	Context("reconcileSkillVolumes", func() {
		It("should add skill volumes to an empty PodSpec", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{{Name: "agent", Image: "test:latest"}},
			}
			skills := []agentv1alpha1.SkillImageRef{
				{Name: "resume-reviewer", Image: "ghcr.io/example/resume-reviewer:v1.0.0", MountPath: "/agent/skills/resume-reviewer"},
			}

			reconcileSkillVolumes(podSpec, skills)

			Expect(podSpec.Volumes).To(HaveLen(1))
			Expect(podSpec.Volumes[0].Name).To(Equal("skill-resume-reviewer"))
			Expect(podSpec.Volumes[0].VolumeSource.Image).NotTo(BeNil())
			Expect(podSpec.Volumes[0].VolumeSource.Image.Reference).To(Equal("ghcr.io/example/resume-reviewer:v1.0.0"))

			Expect(podSpec.Containers[0].VolumeMounts).To(HaveLen(1))
			Expect(podSpec.Containers[0].VolumeMounts[0].Name).To(Equal("skill-resume-reviewer"))
			Expect(podSpec.Containers[0].VolumeMounts[0].MountPath).To(Equal("/agent/skills/resume-reviewer"))
			Expect(podSpec.Containers[0].VolumeMounts[0].ReadOnly).To(BeTrue())
		})

		It("should preserve existing non-skill volumes", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "agent",
					Image: "test:latest",
					VolumeMounts: []corev1.VolumeMount{
						{Name: "config", MountPath: "/etc/config"},
					},
				}},
				Volumes: []corev1.Volume{
					{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "my-config"},
					}}},
				},
			}
			skills := []agentv1alpha1.SkillImageRef{
				{Name: "blog-writer", Image: "ghcr.io/example/blog-writer:latest", MountPath: "/app/skills/blog-writer", PullPolicy: agentv1alpha1.SkillPullAlways},
			}

			reconcileSkillVolumes(podSpec, skills)

			Expect(podSpec.Volumes).To(HaveLen(2))
			Expect(podSpec.Volumes[0].Name).To(Equal("config"))
			Expect(podSpec.Volumes[1].Name).To(Equal("skill-blog-writer"))
			Expect(podSpec.Volumes[1].VolumeSource.Image.PullPolicy).To(Equal(corev1.PullAlways))

			Expect(podSpec.Containers[0].VolumeMounts).To(HaveLen(2))
			Expect(podSpec.Containers[0].VolumeMounts[0].Name).To(Equal("config"))
			Expect(podSpec.Containers[0].VolumeMounts[1].Name).To(Equal("skill-blog-writer"))
			Expect(podSpec.Containers[0].VolumeMounts[1].MountPath).To(Equal("/app/skills/blog-writer"))
		})

		It("should remove stale skill volumes", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "agent",
					Image: "test:latest",
					VolumeMounts: []corev1.VolumeMount{
						{Name: "skill-old-skill", MountPath: "/agent/skills/old-skill", ReadOnly: true},
						{Name: "skill-keep-skill", MountPath: "/agent/skills/keep-skill", ReadOnly: true},
					},
				}},
				Volumes: []corev1.Volume{
					{Name: "skill-old-skill", VolumeSource: corev1.VolumeSource{Image: &corev1.ImageVolumeSource{Reference: "old:v1"}}},
					{Name: "skill-keep-skill", VolumeSource: corev1.VolumeSource{Image: &corev1.ImageVolumeSource{Reference: "keep:v1"}}},
				},
			}
			skills := []agentv1alpha1.SkillImageRef{
				{Name: "keep-skill", Image: "keep:v1", MountPath: "/agent/skills/keep-skill"},
			}

			reconcileSkillVolumes(podSpec, skills)

			Expect(podSpec.Volumes).To(HaveLen(1))
			Expect(podSpec.Volumes[0].Name).To(Equal("skill-keep-skill"))

			Expect(podSpec.Containers[0].VolumeMounts).To(HaveLen(1))
			Expect(podSpec.Containers[0].VolumeMounts[0].Name).To(Equal("skill-keep-skill"))
		})

		It("should remove all skill volumes when desired is nil", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "agent",
					Image: "test:latest",
					VolumeMounts: []corev1.VolumeMount{
						{Name: "config", MountPath: "/etc/config"},
						{Name: "skill-a", MountPath: "/agent/skills/a", ReadOnly: true},
						{Name: "skill-b", MountPath: "/agent/skills/b", ReadOnly: true},
					},
				}},
				Volumes: []corev1.Volume{
					{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "my-config"},
					}}},
					{Name: "skill-a", VolumeSource: corev1.VolumeSource{Image: &corev1.ImageVolumeSource{Reference: "a:v1"}}},
					{Name: "skill-b", VolumeSource: corev1.VolumeSource{Image: &corev1.ImageVolumeSource{Reference: "b:v1"}}},
				},
			}

			reconcileSkillVolumes(podSpec, nil)

			Expect(podSpec.Volumes).To(HaveLen(1))
			Expect(podSpec.Volumes[0].Name).To(Equal("config"))

			Expect(podSpec.Containers[0].VolumeMounts).To(HaveLen(1))
			Expect(podSpec.Containers[0].VolumeMounts[0].Name).To(Equal("config"))
		})

		It("should update skill image reference", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{{Name: "agent", Image: "test:latest"}},
				Volumes: []corev1.Volume{
					{Name: "skill-my-skill", VolumeSource: corev1.VolumeSource{Image: &corev1.ImageVolumeSource{Reference: "old:v1"}}},
				},
			}
			skills := []agentv1alpha1.SkillImageRef{
				{Name: "my-skill", Image: "new:v2", MountPath: "/agent/skills/my-skill"},
			}

			reconcileSkillVolumes(podSpec, skills)

			Expect(podSpec.Volumes).To(HaveLen(1))
			Expect(podSpec.Volumes[0].VolumeSource.Image.Reference).To(Equal("new:v2"))
		})

		It("should only mount skills to the first container", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "agent", Image: "agent:latest"},
					{Name: "sidecar", Image: "sidecar:latest"},
				},
			}
			skills := []agentv1alpha1.SkillImageRef{
				{Name: "skill-a", Image: "a:v1", MountPath: "/app/.claude/skills/skill-a"},
			}

			reconcileSkillVolumes(podSpec, skills)

			Expect(podSpec.Containers[0].VolumeMounts).To(HaveLen(1))
			Expect(podSpec.Containers[0].VolumeMounts[0].MountPath).To(Equal("/app/.claude/skills/skill-a"))
			Expect(podSpec.Containers[1].VolumeMounts).To(BeEmpty())
		})

		It("should not mount skills to injected sidecar containers", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "agent", Image: "my-agent:latest"},
					{Name: "envoy-proxy", Image: "envoyproxy/envoy:v1.30"},
					{Name: "spiffe-helper", Image: "ghcr.io/spiffe/spiffe-helper:latest"},
					{Name: "kagenti-client-registration", Image: "kagenti/client-reg:latest"},
				},
			}
			skills := []agentv1alpha1.SkillImageRef{
				{Name: "resume-reviewer", Image: "ghcr.io/example/resume-reviewer:v1.0.0", MountPath: "/agent/skills/resume-reviewer"},
				{Name: "blog-writer", Image: "ghcr.io/example/blog-writer:latest", MountPath: "/app/.claude/skills/blog-writer"},
			}

			reconcileSkillVolumes(podSpec, skills)

			Expect(podSpec.Containers[0].VolumeMounts).To(HaveLen(2))
			Expect(podSpec.Containers[0].VolumeMounts[0].Name).To(Equal("skill-resume-reviewer"))
			Expect(podSpec.Containers[0].VolumeMounts[1].Name).To(Equal("skill-blog-writer"))
			for _, sidecar := range podSpec.Containers[1:] {
				Expect(sidecar.VolumeMounts).To(BeEmpty(),
					"sidecar %q should not have skill mounts", sidecar.Name)
			}
		})

		It("should set pull policy when specified", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{{Name: "agent", Image: "test:latest"}},
			}
			skills := []agentv1alpha1.SkillImageRef{
				{Name: "s1", Image: "img:latest", MountPath: "/skills/s1", PullPolicy: agentv1alpha1.SkillPullAlways},
				{Name: "s2", Image: "img:v1.0.0", MountPath: "/skills/s2", PullPolicy: agentv1alpha1.SkillPullIfNotPresent},
				{Name: "s3", Image: "img:v2.0.0", MountPath: "/skills/s3"},
			}

			reconcileSkillVolumes(podSpec, skills)

			Expect(podSpec.Volumes).To(HaveLen(3))
			Expect(podSpec.Volumes[0].VolumeSource.Image.PullPolicy).To(Equal(corev1.PullAlways))
			Expect(podSpec.Volumes[1].VolumeSource.Image.PullPolicy).To(Equal(corev1.PullIfNotPresent))
			Expect(podSpec.Volumes[2].VolumeSource.Image.PullPolicy).To(Equal(corev1.PullPolicy("")))
		})

		It("should use different mount paths for different frameworks", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{{Name: "agent", Image: "test:latest"}},
			}
			skills := []agentv1alpha1.SkillImageRef{
				{Name: "claude-skill", Image: "img:v1", MountPath: "/app/.claude/skills/my-skill"},
				{Name: "cursor-skill", Image: "img:v1", MountPath: "/app/.cursor/rules/my-skill"},
			}

			reconcileSkillVolumes(podSpec, skills)

			Expect(podSpec.Containers[0].VolumeMounts).To(HaveLen(2))
			Expect(podSpec.Containers[0].VolumeMounts[0].MountPath).To(Equal("/app/.claude/skills/my-skill"))
			Expect(podSpec.Containers[0].VolumeMounts[1].MountPath).To(Equal("/app/.cursor/rules/my-skill"))
		})
	})

	Context("Config hash includes skills", func() {
		ctx := context.Background()
		const namespace = "default"

		It("should change when skills are added", func() {
			specNoSkills := &agentv1alpha1.AgentRuntimeSpec{
				Type:      agentv1alpha1.RuntimeTypeAgent,
				TargetRef: agentv1alpha1.TargetRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "hash-skills"},
			}
			specWithSkills := &agentv1alpha1.AgentRuntimeSpec{
				Type:      agentv1alpha1.RuntimeTypeAgent,
				TargetRef: agentv1alpha1.TargetRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "hash-skills"},
				Skills: []agentv1alpha1.SkillImageRef{
					{Name: "s1", Image: "img:v1", MountPath: "/skills/s1"},
				},
			}

			r1, _ := ComputeConfigHash(ctx, k8sClient, namespace, specNoSkills)
			r2, _ := ComputeConfigHash(ctx, k8sClient, namespace, specWithSkills)
			Expect(r1.Hash).NotTo(Equal(r2.Hash))
		})

		It("should change when skill image changes", func() {
			spec1 := &agentv1alpha1.AgentRuntimeSpec{
				Type:      agentv1alpha1.RuntimeTypeAgent,
				TargetRef: agentv1alpha1.TargetRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "hash-img"},
				Skills:    []agentv1alpha1.SkillImageRef{{Name: "s1", Image: "img:v1", MountPath: "/skills/s1"}},
			}
			spec2 := &agentv1alpha1.AgentRuntimeSpec{
				Type:      agentv1alpha1.RuntimeTypeAgent,
				TargetRef: agentv1alpha1.TargetRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "hash-img"},
				Skills:    []agentv1alpha1.SkillImageRef{{Name: "s1", Image: "img:v2", MountPath: "/skills/s1"}},
			}

			r1, _ := ComputeConfigHash(ctx, k8sClient, namespace, spec1)
			r2, _ := ComputeConfigHash(ctx, k8sClient, namespace, spec2)
			Expect(r1.Hash).NotTo(Equal(r2.Hash))
		})

		It("should change when skill pull policy changes", func() {
			spec1 := &agentv1alpha1.AgentRuntimeSpec{
				Type:      agentv1alpha1.RuntimeTypeAgent,
				TargetRef: agentv1alpha1.TargetRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "hash-pp"},
				Skills:    []agentv1alpha1.SkillImageRef{{Name: "s1", Image: "img:v1", MountPath: "/skills/s1", PullPolicy: agentv1alpha1.SkillPullAlways}},
			}
			spec2 := &agentv1alpha1.AgentRuntimeSpec{
				Type:      agentv1alpha1.RuntimeTypeAgent,
				TargetRef: agentv1alpha1.TargetRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "hash-pp"},
				Skills:    []agentv1alpha1.SkillImageRef{{Name: "s1", Image: "img:v1", MountPath: "/skills/s1", PullPolicy: agentv1alpha1.SkillPullIfNotPresent}},
			}

			r1, _ := ComputeConfigHash(ctx, k8sClient, namespace, spec1)
			r2, _ := ComputeConfigHash(ctx, k8sClient, namespace, spec2)
			Expect(r1.Hash).NotTo(Equal(r2.Hash))
		})

		It("should change when skill mount path changes", func() {
			spec1 := &agentv1alpha1.AgentRuntimeSpec{
				Type:      agentv1alpha1.RuntimeTypeAgent,
				TargetRef: agentv1alpha1.TargetRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "hash-mp"},
				Skills:    []agentv1alpha1.SkillImageRef{{Name: "s1", Image: "img:v1", MountPath: "/agent/skills/s1"}},
			}
			spec2 := &agentv1alpha1.AgentRuntimeSpec{
				Type:      agentv1alpha1.RuntimeTypeAgent,
				TargetRef: agentv1alpha1.TargetRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "hash-mp"},
				Skills:    []agentv1alpha1.SkillImageRef{{Name: "s1", Image: "img:v1", MountPath: "/app/.claude/skills/s1"}},
			}

			r1, _ := ComputeConfigHash(ctx, k8sClient, namespace, spec1)
			r2, _ := ComputeConfigHash(ctx, k8sClient, namespace, spec2)
			Expect(r1.Hash).NotTo(Equal(r2.Hash))
		})
	})
})
