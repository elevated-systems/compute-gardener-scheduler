#!/usr/bin/env python3
"""
Hyperparameter Sweep Generator for Carbon-Aware LoRA Fine-tuning

Generates RayJob manifests with three-tier carbon-aware scheduling:
- Tier 1: Baseline variations (fast feedback needed)
- Tier 2: Promising directions (moderate delay tolerance)
- Tier 3: Experimental long shots (maximum carbon optimization)
"""

import argparse
import itertools
from pathlib import Path
from typing import Dict, List, Tuple

try:
    import yaml
except ImportError:
    print("Error: PyYAML is required but not installed.")
    print("\nTo install on macOS with Homebrew Python:")
    print("  pip3 install pyyaml")
    print("  # or")
    print("  python3 -m pip install pyyaml")
    print("\nThe brew warning can be ignored - pip3 will work fine.")
    exit(1)


class ExperimentTier:
    """Defines carbon-aware scheduling tiers"""
    BASELINE = 1      # High priority, threshold=225, max-delay=24h
    PROMISING = 2     # Medium priority, threshold=175, max-delay=48h
    LONGSHOT = 3      # Low priority, threshold=125, max-delay=96h


def assign_tier(config: Dict) -> Tuple[int, str]:
    """
    Assign carbon scheduling tier based on experimental characteristics

    Returns: (tier_number, rationale)
    """
    r = config['r']
    alpha = config['alpha']
    lr = config['lr']
    dropout = config['dropout']

    # Tier 1: Baseline variations (r=16, known good configs)
    if r == 16:
        return (ExperimentTier.BASELINE,
                "baseline-variation: r=16 proven effective, needs fast feedback")

    # Tier 2 vs 3: Both use r=32, differentiate by hyperparameters
    elif r == 32:
        # Tier 3: Experimental/aggressive settings (longshots)
        if alpha == 128 or dropout >= 0.15 or lr <= 1e-5:
            return (ExperimentTier.LONGSHOT,
                    "longshot: r=32 with aggressive/unconventional settings")
        # Tier 2: Standard promising directions
        else:
            return (ExperimentTier.PROMISING,
                    "promising: r=32 with standard hyperparameters")

    # Fallback for unusual configurations
    else:
        return (ExperimentTier.PROMISING,
                f"promising: r={r} unusual configuration")


def get_tier_config(tier: int) -> Dict:
    """Get carbon scheduling configuration for tier"""
    configs = {
        ExperimentTier.BASELINE: {
            'threshold': 225.0,
            'max_delay': '24h',
            'description': 'Baseline variations - fast iteration'
        },
        ExperimentTier.PROMISING: {
            'threshold': 175.0,
            'max_delay': '48h',
            'description': 'Promising directions - balanced approach'
        },
        ExperimentTier.LONGSHOT: {
            'threshold': 125.0,
            'max_delay': '96h',
            'description': 'Experimental longshots - maximum carbon optimization'
        }
    }
    return configs[tier]


def generate_rayjob_manifest(
    config: Dict,
    tier: int,
    tier_rationale: str,
    namespace: str = "ray-jobs",
    image: str = "dmasselink/ray-ml-modern:2.30.0"
) -> Dict:
    """Generate a RayJob manifest for the given configuration"""
    
    tier_cfg = get_tier_config(tier)
    
    # Create unique job name
    job_name = f"qwen-lora-r{config['r']}-a{config['alpha']}-lr{config['lr']:.0e}-d{int(config['dropout']*100)}-tier{tier}"
    
    # Base manifest structure
    manifest = {
        'apiVersion': 'ray.io/v1',
        'kind': 'RayJob',
        'metadata': {
            'name': job_name,
            'namespace': namespace,
            'annotations': {
                'compute-gardener.io/job-type': 'llm-finetuning',
                'compute-gardener.io/technique': 'lora',
                'compute-gardener.io/tier': str(tier),
                'compute-gardener.io/tier-rationale': tier_rationale,
                'compute-gardener.io/config': f"r={config['r']},alpha={config['alpha']},lr={config['lr']},dropout={config['dropout']}"
            }
        },
        'spec': {
            'entrypoint': (
                f"python /app/train_lora.py "
                f"--r {config['r']} "
                f"--alpha {config['alpha']} "
                f"--lr {config['lr']} "
                f"--dropout {config['dropout']}"
            ),
            'shutdownAfterJobFinishes': True,
            'clusterSelector': {},
            'rayClusterSpec': {
                'rayVersion': '2.30.0',
                'enableInTreeAutoscaling': True,
                'headGroupSpec': {
                    'serviceType': 'ClusterIP',
                    'rayStartParams': {
                        'dashboard-host': '0.0.0.0',
                        'num-cpus': '0'
                    },
                    'template': {
                        'metadata': {
                            'annotations': {
                                'compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold': 
                                    str(tier_cfg['threshold']),
                                'compute-gardener-scheduler.kubernetes.io/max-scheduling-delay': 
                                    tier_cfg['max_delay']
                            }
                        },
                        'spec': {
                            'schedulerName': 'compute-gardener-scheduler',
                            'containers': [{
                                'name': 'ray-head',
                                'image': image,
                                'ports': [
                                    {'containerPort': 6379, 'name': 'gcs'},
                                    {'containerPort': 8265, 'name': 'dashboard'},
                                    {'containerPort': 10001, 'name': 'client'},
                                    {'containerPort': 8000, 'name': 'serve'}
                                ],
                                'resources': {
                                    'limits': {'cpu': '2', 'memory': '4Gi'},
                                    'requests': {'cpu': '500m', 'memory': '2Gi'}
                                },
                                'env': [{'name': 'RAY_LOG_LEVEL', 'value': 'INFO'}],
                                'volumeMounts': [
                                    {'name': 'training-script', 'mountPath': '/app'},
                                    {'name': 'ray-logs', 'mountPath': '/tmp/ray'}
                                ]
                            }],
                            'volumes': [
                                {
                                    'name': 'training-script',
                                    'configMap': {
                                        'name': 'llm-finetuning-script',
                                        'defaultMode': 0o755
                                    }
                                },
                                {'name': 'ray-logs', 'emptyDir': {}}
                            ]
                        }
                    }
                },
                'workerGroupSpecs': [{
                    'replicas': 1,
                    'minReplicas': 0,
                    'maxReplicas': 1,
                    'groupName': 'gpu-worker',
                    'rayStartParams': {'num-gpus': '1'},
                    'template': {
                        'metadata': {
                            'annotations': {
                                'compute-gardener-scheduler.kubernetes.io/carbon-intensity-threshold': 
                                    str(tier_cfg['threshold']),
                                'compute-gardener-scheduler.kubernetes.io/max-scheduling-delay': 
                                    tier_cfg['max_delay']
                            }
                        },
                        'spec': {
                            'schedulerName': 'compute-gardener-scheduler',
                            'runtimeClassName': 'nvidia',
                            'securityContext': {
                                'fsGroup': 1000,
                                'runAsUser': 1000,
                                'runAsGroup': 1000
                            },
                            'containers': [{
                                'name': 'ray-worker',
                                'image': image,
                                'resources': {
                                    'limits': {
                                        'nvidia.com/gpu': 1,
                                        'cpu': '8',
                                        'memory': '32Gi'
                                    },
                                    'requests': {
                                        'nvidia.com/gpu': 1,
                                        'cpu': '4',
                                        'memory': '24Gi'
                                    }
                                },
                                'env': [
                                    {'name': 'RAY_LOG_LEVEL', 'value': 'INFO'},
                                    {'name': 'NVIDIA_VISIBLE_DEVICES', 'value': 'all'},
                                    {'name': 'NVIDIA_DRIVER_CAPABILITIES', 'value': 'compute,utility'},
                                    {'name': 'HF_HOME', 'value': '/mnt/models/.cache'},
                                    {'name': 'HF_HUB_CACHE', 'value': '/mnt/models/.cache/huggingface/hub'}
                                ],
                                'volumeMounts': [
                                    {'name': 'training-script', 'mountPath': '/app'},
                                    {'name': 'ray-logs', 'mountPath': '/tmp/ray'},
                                    {'name': 'dshm', 'mountPath': '/dev/shm'},
                                    {'name': 'model-storage', 'mountPath': '/mnt/models'}
                                ]
                            }],
                            'volumes': [
                                {
                                    'name': 'training-script',
                                    'configMap': {
                                        'name': 'llm-finetuning-script',
                                        'defaultMode': 0o755
                                    }
                                },
                                {'name': 'ray-logs', 'emptyDir': {}},
                                {
                                    'name': 'dshm',
                                    'emptyDir': {
                                        'medium': 'Memory',
                                        'sizeLimit': '4Gi'
                                    }
                                },
                                {
                                    'name': 'model-storage',
                                    'persistentVolumeClaim': {
                                        'claimName': 'llm-finetuned-models-rwx'
                                    }
                                }
                            ],
                            'nodeSelector': {'nvidia.com/gpu.present': 'true'},
                            'tolerations': [{
                                'key': 'nvidia.com/gpu',
                                'operator': 'Exists',
                                'effect': 'NoSchedule'
                            }]
                        }
                    }
                }]
            },
            'submitterPodTemplate': {
                'spec': {
                    'containers': [{
                        'name': 'job-submitter',
                        'image': image,
                        'resources': {
                            'limits': {'cpu': '500m', 'memory': '1Gi'},
                            'requests': {'cpu': '100m', 'memory': '512Mi'}
                        }
                    }],
                    'restartPolicy': 'Never'
                }
            },
            'ttlSecondsAfterFinished': 36000,  # 10 hours
            'activeDeadlineSeconds': 86400    # 24 hours max
        }
    }
    
    return manifest


def generate_sweep(
    output_dir: Path,
    r_values: List[int],
    alpha_values: List[int],
    lr_values: List[float],
    dropout_values: List[float],
    sample_size: int = None,
    core_subset: bool = False
) -> List[Dict]:
    """Generate hyperparameter sweep configurations"""

    # Use curated core subset for interpretable OFAT exploration
    if core_subset:
        # Structured one-factor-at-a-time (OFAT) exploration for interpretability
        # Baseline: r=16, alpha=32, lr=1e-4, dropout=0.1
        # Systematically vary one parameter at a time from baseline

        targeted_configs = [
            # Tier 1: r=16 baseline variations (8 experiments)
            {'r': 16, 'alpha': 32, 'lr': 1e-4, 'dropout': 0.1},   # baseline
            {'r': 16, 'alpha': 64, 'lr': 1e-4, 'dropout': 0.1},   # vary alpha
            {'r': 16, 'alpha': 128, 'lr': 1e-4, 'dropout': 0.1},  # vary alpha more
            {'r': 16, 'alpha': 32, 'lr': 5e-5, 'dropout': 0.1},   # vary lr
            {'r': 16, 'alpha': 32, 'lr': 1e-5, 'dropout': 0.1},   # vary lr more
            {'r': 16, 'alpha': 32, 'lr': 1e-4, 'dropout': 0.05},  # vary dropout
            {'r': 16, 'alpha': 32, 'lr': 1e-4, 'dropout': 0.15},  # vary dropout more
            {'r': 16, 'alpha': 64, 'lr': 5e-5, 'dropout': 0.1},   # combined promising

            # Tier 2: r=32 promising directions (4 experiments)
            {'r': 32, 'alpha': 32, 'lr': 1e-4, 'dropout': 0.1},   # step up from baseline
            {'r': 32, 'alpha': 64, 'lr': 1e-4, 'dropout': 0.05},  # lower dropout
            {'r': 32, 'alpha': 64, 'lr': 1e-4, 'dropout': 0.1},   # vary alpha
            {'r': 32, 'alpha': 64, 'lr': 5e-5, 'dropout': 0.1},   # conservative lr
            
            # Tier 3: r=32 aggressive/experimental (9 experiments)
            # Note: r=64 causes OOM on 24GB VRAM, so Tier 3 uses r=32 with more experimental settings
            {'r': 32, 'alpha': 128, 'lr': 1e-4, 'dropout': 0.1},  # vary alpha more
            {'r': 32, 'alpha': 64, 'lr': 1e-5, 'dropout': 0.1},   # very conservative lr
            {'r': 32, 'alpha': 64, 'lr': 1e-4, 'dropout': 0.15},  # higher dropout
            {'r': 32, 'alpha': 128, 'lr': 5e-5, 'dropout': 0.1},  # aggressive + conservative
            {'r': 32, 'alpha': 128, 'lr': 1e-4, 'dropout': 0.05}, # high alpha + low dropout
            {'r': 32, 'alpha': 128, 'lr': 1e-4, 'dropout': 0.15}, # high alpha + high dropout
            {'r': 32, 'alpha': 32, 'lr': 1e-4, 'dropout': 0.15},  # baseline alpha + high dropout
            {'r': 32, 'alpha': 128, 'lr': 1e-5, 'dropout': 0.1},  # very conservative lr + high alpha
            {'r': 32, 'alpha': 32, 'lr': 5e-5, 'dropout': 0.15},  # conservative combo
        ]

        # Convert to the expected format with tier assignments
        all_configs = []
        for config in targeted_configs:
            tier, rationale = assign_tier(config)
            all_configs.append((config, tier, rationale))

    else:
        # Generate all combinations from parameter grid
        all_configs = []
        for r, alpha, lr, dropout in itertools.product(
            r_values, alpha_values, lr_values, dropout_values
        ):
            config = {
                'r': r,
                'alpha': alpha,
                'lr': lr,
                'dropout': dropout
            }
            tier, rationale = assign_tier(config)
            all_configs.append((config, tier, rationale))

        # Optionally sample for a smaller sweep
        if sample_size and sample_size < len(all_configs):
            # Proportional random sampling
            tier1 = [c for c in all_configs if c[1] == ExperimentTier.BASELINE]
            tier2 = [c for c in all_configs if c[1] == ExperimentTier.PROMISING]
            tier3 = [c for c in all_configs if c[1] == ExperimentTier.LONGSHOT]

            n1 = min(len(tier1), sample_size // 3)
            n2 = min(len(tier2), sample_size // 3)
            n3 = min(len(tier3), sample_size - n1 - n2)

            import random
            random.seed(42)
            sampled = (
                random.sample(tier1, min(n1, len(tier1))) +
                random.sample(tier2, min(n2, len(tier2))) +
                random.sample(tier3, min(n3, len(tier3)))
            )
            all_configs = sampled
    
    # Generate manifests
    output_dir.mkdir(parents=True, exist_ok=True)
    manifests = []
    
    for config, tier, rationale in all_configs:
        manifest = generate_rayjob_manifest(config, tier, rationale)
        manifests.append(manifest)
        
        # Write to file as YAML
        filename = manifest['metadata']['name'] + '.yaml'
        with open(output_dir / filename, 'w') as f:
            yaml.dump(manifest, f, default_flow_style=False, sort_keys=False)
    
    print(f"Generated {len(manifests)} RayJob manifests in {output_dir}")
    
    # Print tier distribution
    tier_counts = {}
    for _, tier, _ in all_configs:
        tier_counts[tier] = tier_counts.get(tier, 0) + 1
    
    print("\nTier Distribution:")
    for tier in sorted(tier_counts.keys()):
        tier_cfg = get_tier_config(tier)
        print(f"  Tier {tier} ({tier_cfg['description']}): {tier_counts[tier]} experiments")
        print(f"    - Carbon threshold: {tier_cfg['threshold']} gCO2/kWh")
        print(f"    - Max delay: {tier_cfg['max_delay']}")
    
    return manifests


def main():
    parser = argparse.ArgumentParser(
        description='Generate carbon-aware LoRA hyperparameter sweep'
    )
    parser.add_argument(
        '--output-dir',
        type=Path,
        default=Path('sweep_manifests'),
        help='Output directory for generated manifests'
    )
    parser.add_argument(
        '--r-values',
        type=int,
        nargs='+',
        default=[16, 32],
        help='LoRA rank values to sweep (r=64 causes OOM on 24GB VRAM)'
    )
    parser.add_argument(
        '--alpha-values',
        type=int,
        nargs='+',
        default=[16, 32, 64],
        help='LoRA alpha values to sweep'
    )
    parser.add_argument(
        '--lr-values',
        type=float,
        nargs='+',
        default=[1e-4, 5e-5, 1e-5],
        help='Learning rate values to sweep'
    )
    parser.add_argument(
        '--dropout-values',
        type=float,
        nargs='+',
        default=[0.05, 0.1, 0.15],
        help='Dropout values to sweep'
    )
    parser.add_argument(
        '--sample-size',
        type=int,
        help='Sample N configs from the full sweep (default: use all)'
    )
    parser.add_argument(
        '--core-subset',
        action='store_true',
        help='Use curated core subset (21 experiments) with OFAT exploration from baseline'
    )
    
    args = parser.parse_args()
    
    print(f"Generating hyperparameter sweep...")
    print(f"  Rank: {args.r_values}")
    print(f"  Alpha: {args.alpha_values}")
    print(f"  Learning rate: {args.lr_values}")
    print(f"  Dropout: {args.dropout_values}")
    
    total_combinations = (
        len(args.r_values) *
        len(args.alpha_values) *
        len(args.lr_values) *
        len(args.dropout_values)
    )
    print(f"\nTotal possible combinations: {total_combinations}")
    
    if args.sample_size:
        print(f"Sampling {args.sample_size} configurations")
    
    manifests = generate_sweep(
        args.output_dir,
        args.r_values,
        args.alpha_values,
        args.lr_values,
        args.dropout_values,
        args.sample_size,
        args.core_subset
    )
    
    print(f"\n✅ Sweep generation complete!")
    print(f"\nTo submit all experiments:")
    print(f"  kubectl apply -f {args.output_dir}/")
    print(f"\nTo submit one tier at a time:")
    print(f"  for f in {args.output_dir}/*-tier1.yaml; do kubectl apply -f \"$f\"; done  # Baselines first")
    print(f"  for f in {args.output_dir}/*-tier2.yaml; do kubectl apply -f \"$f\"; done  # Then promising")
    print(f"  for f in {args.output_dir}/*-tier3.yaml; do kubectl apply -f \"$f\"; done  # Finally long shots")


if __name__ == '__main__':
    main()
