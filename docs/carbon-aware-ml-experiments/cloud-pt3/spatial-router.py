#!/usr/bin/env python3
"""
Carbon-Aware Spatial Router for Part 3 Experiment

Routes LoRA fine-tuning jobs to the cleanest available region based on
real-time carbon intensity data from Electricity Maps.

Usage:
    # Run the full experiment on a 4-hour schedule
    python spatial_router.py --run-experiment
    
    # Submit a single job to the cleanest region
    python spatial_router.py --submit-single --r 16 --alpha 64 --lr 1e-4
    
    # Check current carbon intensity across regions (no job submission)
    python spatial_router.py --check-intensity

Configuration:
    Set ELECTRICITY_MAPS_TOKEN environment variable or update the constant below.
    Update REGIONS dict with your Vast.ai instance IPs (Tailscale or public).
"""

import argparse
import json
import os
import subprocess
import time
from datetime import datetime, timedelta
from pathlib import Path
from typing import Dict, Tuple, Optional
import requests


# ============================================================================
# Configuration
# ============================================================================

ELECTRICITY_MAPS_TOKEN = os.environ.get("ELECTRICITY_MAPS_TOKEN", "YOUR_TOKEN_HERE")

# Region configuration
# Update IPs with your actual Vast.ai instance addresses (Tailscale recommended)
REGIONS: Dict[str, Dict] = {
    "quebec": {
        "zone": "CA-QC",           # Hydro-Quebec - very clean hydro
        "ip": "100.65.196.126",         # Tailscale IP for Quebec instance
        "description": "Quebec, Canada - Hydro-dominated grid",
    },
    "ohio": {
        "zone": "US-MIDA-PJM",          # PJM - includes Ohio area - coal/gas heavy
        "ip": "100.x.x.x",         # Tailscale IP for Ohio instance  
        "description": "Ohio, USA - Fossil-heavy grid (baseline)",
    },
    "nordic": {
        "zone": "NO",
        # Alternative zones if using different Nordic locations:
        # "zone": "IS",            # Iceland - geothermal/hydro
        # "zone": "SE",            # Sweden - hydro/nuclear
        # "zone": "NO",            # Norway - hydro dominated
        # "zone": "IE",            # Ireland (not nordic) - dirtier grid, for testing/forcing other regions
        "ip": "100.80.123.80",         # Tailscale IP for Nordic instance
        "description": "Nordic - Hydro-dominated grid",
    },
}

# Experiment configuration
EXPERIMENT_CONFIGS = [
    # Tier 1 equivalent: r=16 baseline variations
    {'r': 16, 'alpha': 32, 'lr': 1e-4, 'dropout': 0.1},
    {'r': 16, 'alpha': 64, 'lr': 1e-4, 'dropout': 0.1},
    {'r': 16, 'alpha': 128, 'lr': 1e-4, 'dropout': 0.1},
    {'r': 16, 'alpha': 32, 'lr': 5e-5, 'dropout': 0.1},
    {'r': 16, 'alpha': 64, 'lr': 5e-5, 'dropout': 0.1},
    {'r': 16, 'alpha': 32, 'lr': 1e-4, 'dropout': 0.05},
    {'r': 16, 'alpha': 32, 'lr': 1e-4, 'dropout': 0.15},
    {'r': 16, 'alpha': 32, 'lr': 1e-5, 'dropout': 0.1},
    
    # Tier 2/3 equivalent: r=32 variations  
    {'r': 32, 'alpha': 32, 'lr': 1e-4, 'dropout': 0.1},
    {'r': 32, 'alpha': 32, 'lr': 1e-4, 'dropout': 0.15},
    {'r': 32, 'alpha': 32, 'lr': 5e-5, 'dropout': 0.15},
    {'r': 32, 'alpha': 64, 'lr': 1e-4, 'dropout': 0.05},
    {'r': 32, 'alpha': 64, 'lr': 1e-4, 'dropout': 0.1},
    {'r': 32, 'alpha': 64, 'lr': 1e-4, 'dropout': 0.15},
    {'r': 32, 'alpha': 64, 'lr': 1e-5, 'dropout': 0.1},
    {'r': 32, 'alpha': 128, 'lr': 1e-4, 'dropout': 0.1},
    {'r': 32, 'alpha': 64, 'lr': 5e-5, 'dropout': 0.1},
    {'r': 32, 'alpha': 128, 'lr': 5e-5, 'dropout': 0.1},
    {'r': 32, 'alpha': 128, 'lr': 1e-5, 'dropout': 0.1},
    {'r': 32, 'alpha': 128, 'lr': 1e-4, 'dropout': 0.05},
    {'r': 32, 'alpha': 128, 'lr': 1e-4, 'dropout': 0.15},
]

# Submission schedule: every 2 hours, 21 jobs, better part of 2 days
SUBMISSION_INTERVAL_HOURS = 2
TOTAL_JOBS = len(EXPERIMENT_CONFIGS)

# SSH configuration
SSH_USER = "root"  # Vast.ai default
SSH_KEY_PATH = os.path.expanduser(
    os.environ.get("SSH_KEY_PATH", "~/.ssh/id_ed25519")
)  # Auto-detect or use SSH_KEY_PATH env var
REMOTE_SCRIPT_PATH = "~/training/train_lora_cloud.py"
PYTHON_CMD = "python3"  # Most cloud instances use python3

# Logging
LOG_DIR = Path("./experiment_logs")
LOG_DIR.mkdir(exist_ok=True)


# ============================================================================
# Carbon Intensity API
# ============================================================================

def get_carbon_intensity(zone: str) -> Optional[float]:
    """Query Electricity Maps for current carbon intensity."""
    try:
        response = requests.get(
            "https://api.electricitymap.org/v3/carbon-intensity/latest",
            params={"zone": zone},
            headers={"auth-token": ELECTRICITY_MAPS_TOKEN},
            timeout=10,
        )
        response.raise_for_status()
        return response.json()["carbonIntensity"]
    except requests.exceptions.RequestException as e:
        print(f"  ⚠️  API error for zone {zone}: {e}")
        return None


def get_all_intensities() -> Dict[str, Optional[float]]:
    """Get carbon intensity for all configured regions."""
    intensities = {}
    for region, config in REGIONS.items():
        intensity = get_carbon_intensity(config["zone"])
        intensities[region] = intensity
        time.sleep(0.5)  # Rate limiting courtesy
    return intensities


def pick_cleanest_region() -> Tuple[str, Dict[str, Optional[float]]]:
    """Return the region with lowest current carbon intensity."""
    intensities = get_all_intensities()
    
    # Filter out failed API calls
    valid = {k: v for k, v in intensities.items() if v is not None}
    
    if not valid:
        raise RuntimeError("Could not get carbon intensity for any region!")
    
    cleanest = min(valid, key=valid.get)
    return cleanest, intensities


def print_intensity_report(intensities: Dict[str, Optional[float]], selected: str = None):
    """Print a formatted carbon intensity report."""
    print(f"\n📊 Carbon Intensity Report ({datetime.now().strftime('%Y-%m-%d %H:%M:%S UTC')})")
    print("-" * 60)
    
    # Sort by intensity (lowest first)
    sorted_regions = sorted(
        [(r, i) for r, i in intensities.items() if i is not None],
        key=lambda x: x[1]
    )
    
    for region, intensity in sorted_regions:
        config = REGIONS[region]
        marker = " ← SELECTED" if region == selected else ""
        status = "🟢" if intensity < 100 else "🟡" if intensity < 300 else "🔴"
        print(f"  {status} {region:10} {intensity:6.1f} gCO2/kWh  ({config['zone']}){marker}")
    
    # Show failed regions
    failed = [r for r, i in intensities.items() if i is None]
    for region in failed:
        print(f"  ⚠️  {region:10} API ERROR")
    
    print("-" * 60)


# ============================================================================
# Job Submission
# ============================================================================

def generate_job_name(config: Dict) -> str:
    """Generate a unique job name from hyperparameters."""
    return f"lora-r{config['r']}-a{config['alpha']}-lr{config['lr']:.0e}-d{int(config['dropout']*100)}"


def submit_job_ssh(region: str, config: Dict, intensities: Dict, job_index: int) -> Dict:
    """Submit a training job to the specified region via SSH."""
    
    job_name = generate_job_name(config)
    job_id = f"{job_name}-{datetime.now().strftime('%Y%m%d%H%M%S')}"
    
    region_config = REGIONS[region]
    ip = region_config["ip"]
    intensity = intensities.get(region, "unknown")
    
    print(f"\n🚀 Submitting job to {region}...")
    print(f"   Job: {job_name}")
    print(f"   IP: {ip}")
    print(f"   Carbon intensity: {intensity} gCO2/kWh")
    
    # Build the remote command with output logging
    log_file = f"~/training/logs/{job_id}.log"
    remote_cmd = (
        f"mkdir -p ~/training/logs && "
        f"cd ~/training && "
        f"("
        f"CARBON_REGION={region} "
        f"CARBON_INTENSITY={intensity} "
        f"JOB_ID={job_id} "
        f"{PYTHON_CMD} train_lora_cloud.py "
        f"--r {config['r']} "
        f"--alpha {config['alpha']} "
        f"--lr {config['lr']} "
        f"--dropout {config['dropout']} "
        f") 2>&1 | tee {log_file}"
    )

    # Build SSH command
    ssh_cmd = [
        "ssh",
        "-i", SSH_KEY_PATH,
        "-o", "StrictHostKeyChecking=no",
        "-o", "UserKnownHostsFile=/dev/null",
        "-o", "ConnectTimeout=30",
        f"{SSH_USER}@{ip}",
        remote_cmd,
    ]
    
    # Log the submission
    log_entry = {
        "timestamp": datetime.now().isoformat(),
        "job_index": job_index,
        "job_id": job_id,
        "job_name": job_name,
        "config": config,
        "selected_region": region,
        "intensities": {k: v for k, v in intensities.items()},  # Copy to avoid mutation
        "region_zone": region_config["zone"],
        "status": "submitted",
    }
    
    # Execute (this will block until training completes)
    print(f"   Executing: ssh {SSH_USER}@{ip} ...")
    print(f"   📋 Remote log: {log_file}")
    print(f"   💡 To watch progress: ssh -i {SSH_KEY_PATH} {SSH_USER}@{ip} 'tail -f {log_file}'")
    start_time = time.time()
    
    try:
        result = subprocess.run(
            ssh_cmd,
            capture_output=True,
            text=True,
            timeout=4 * 3600,  # 4 hour timeout per job
        )
        
        elapsed = time.time() - start_time
        log_entry["elapsed_seconds"] = elapsed
        log_entry["elapsed_hours"] = elapsed / 3600
        
        if result.returncode == 0:
            log_entry["status"] = "completed"
            print(f"   ✅ Job completed in {elapsed/3600:.2f} hours")
        else:
            log_entry["status"] = "failed"
            log_entry["error"] = result.stderr[-1000:] if result.stderr else "Unknown error"
            print(f"   ❌ Job failed after {elapsed/3600:.2f} hours")
            print(f"   Error: {result.stderr[-500:]}")
            
    except subprocess.TimeoutExpired:
        log_entry["status"] = "timeout"
        print(f"   ⏰ Job timed out after 4 hours")
    except Exception as e:
        log_entry["status"] = "error"
        log_entry["error"] = str(e)
        print(f"   ❌ Error: {e}")
    
    return log_entry


def submit_job_async(region: str, config: Dict, intensities: Dict, job_index: int) -> Dict:
    """Submit a training job asynchronously (returns immediately, job runs in background)."""
    
    job_name = generate_job_name(config)
    job_id = f"{job_name}-{datetime.now().strftime('%Y%m%d%H%M%S')}"
    
    region_config = REGIONS[region]
    ip = region_config["ip"]
    intensity = intensities.get(region, "unknown")
    
    print(f"\n🚀 Submitting job to {region} (async)...")
    print(f"   Job: {job_name}")
    print(f"   IP: {ip}")
    print(f"   Carbon intensity: {intensity} gCO2/kWh")
    
    # Build the remote command with nohup for background execution
    remote_cmd = (
        f"cd ~/training && "
        f"nohup bash -c '"
        f"CARBON_REGION={region} "
        f"CARBON_INTENSITY={intensity} "
        f"JOB_ID={job_id} "
        f"{PYTHON_CMD} train_lora_cloud.py "
        f"--r {config['r']} "
        f"--alpha {config['alpha']} "
        f"--lr {config['lr']} "
        f"--dropout {config['dropout']} "
        f"' > ~/training/logs/{job_id}.log 2>&1 &"
    )

    # Build SSH command
    ssh_cmd = [
        "ssh",
        "-i", SSH_KEY_PATH,
        "-o", "StrictHostKeyChecking=no",
        "-o", "UserKnownHostsFile=/dev/null",
        "-o", "ConnectTimeout=30",
        f"{SSH_USER}@{ip}",
        f"mkdir -p ~/training/logs && {remote_cmd}",
    ]
    
    # Log the submission
    log_entry = {
        "timestamp": datetime.now().isoformat(),
        "job_index": job_index,
        "job_id": job_id,
        "job_name": job_name,
        "config": config,
        "selected_region": region,
        "intensities": {k: v for k, v in intensities.items()},
        "region_zone": region_config["zone"],
        "status": "submitted_async",
    }
    
    try:
        result = subprocess.run(
            ssh_cmd,
            capture_output=True,
            text=True,
            timeout=60,  # Just for the SSH connection
        )
        
        if result.returncode == 0:
            print(f"   ✅ Job submitted (running in background)")
            print(f"   📋 Log: ~/training/logs/{job_id}.log")
        else:
            log_entry["status"] = "submit_failed"
            log_entry["error"] = result.stderr
            print(f"   ❌ Failed to submit: {result.stderr}")
            
    except Exception as e:
        log_entry["status"] = "error"
        log_entry["error"] = str(e)
        print(f"   ❌ Error: {e}")
    
    return log_entry


# ============================================================================
# Experiment Runner
# ============================================================================

def run_experiment(async_mode: bool = False):
    """Run the full experiment on a fixed schedule."""
    
    experiment_id = datetime.now().strftime('%Y%m%d_%H%M%S')
    log_file = LOG_DIR / f"experiment_{experiment_id}.jsonl"
    
    print(f"\n{'='*60}")
    print(f"🌱 Carbon-Aware Spatial Shifting Experiment")
    print(f"{'='*60}")
    print(f"Experiment ID: {experiment_id}")
    print(f"Total jobs: {TOTAL_JOBS}")
    print(f"Submission interval: {SUBMISSION_INTERVAL_HOURS} hours")
    print(f"Expected duration: {TOTAL_JOBS * SUBMISSION_INTERVAL_HOURS} hours ({TOTAL_JOBS * SUBMISSION_INTERVAL_HOURS / 24:.1f} days)")
    print(f"Log file: {log_file}")
    print(f"Mode: {'async (background)' if async_mode else 'sync (blocking)'}")
    print(f"{'='*60}\n")
    
    # Verify regions are configured
    unconfigured = [r for r, c in REGIONS.items() if "100.x.x.x" in c["ip"]]
    if unconfigured:
        print(f"⚠️  WARNING: The following regions have placeholder IPs: {unconfigured}")
        print(f"   Update REGIONS dict with actual Vast.ai/Tailscale IPs before running.")
        response = input("Continue anyway? (y/N): ")
        if response.lower() != 'y':
            return
    
    submit_func = submit_job_async if async_mode else submit_job_ssh
    
    for i, config in enumerate(EXPERIMENT_CONFIGS):
        print(f"\n{'='*60}")
        print(f"📋 Job {i+1}/{TOTAL_JOBS} at {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
        print(f"{'='*60}")
        
        # Pick cleanest region
        try:
            region, intensities = pick_cleanest_region()
        except RuntimeError as e:
            print(f"❌ {e}")
            print("   Skipping this submission window, will retry next interval.")
            
            # Log the failure
            log_entry = {
                "timestamp": datetime.now().isoformat(),
                "job_index": i,
                "config": config,
                "status": "skipped_api_error",
            }
            with open(log_file, 'a') as f:
                f.write(json.dumps(log_entry) + "\n")
            
            # Wait and continue
            if i < len(EXPERIMENT_CONFIGS) - 1:
                wait_until_next_submission(SUBMISSION_INTERVAL_HOURS)
            continue
        
        print_intensity_report(intensities, selected=region)
        
        # Submit job
        log_entry = submit_func(region, config, intensities, i)
        
        # Save log entry
        with open(log_file, 'a') as f:
            f.write(json.dumps(log_entry) + "\n")
        
        # Wait for next submission (unless this is the last job)
        if i < len(EXPERIMENT_CONFIGS) - 1:
            wait_until_next_submission(SUBMISSION_INTERVAL_HOURS)
    
    print(f"\n{'='*60}")
    print(f"✅ Experiment complete!")
    print(f"   Log file: {log_file}")
    print(f"{'='*60}\n")


def wait_until_next_submission(hours: float):
    """Wait until the next submission window."""
    next_time = datetime.now() + timedelta(hours=hours)
    print(f"\n⏳ Next submission at {next_time.strftime('%Y-%m-%d %H:%M:%S')}")
    print(f"   Waiting {hours} hours...")
    
    # Sleep in chunks so we can show progress
    total_seconds = hours * 3600
    chunk_seconds = 600  # Update every 10 minutes
    
    elapsed = 0
    while elapsed < total_seconds:
        sleep_time = min(chunk_seconds, total_seconds - elapsed)
        time.sleep(sleep_time)
        elapsed += sleep_time
        
        remaining = total_seconds - elapsed
        if remaining > 0:
            remaining_hrs = remaining / 3600
            print(f"   ... {remaining_hrs:.1f} hours remaining")


# ============================================================================
# Utility Commands
# ============================================================================

def check_intensity_only():
    """Just check and report current carbon intensity."""
    print("\nFetching current carbon intensity...")
    intensities = get_all_intensities()
    
    # Find cleanest
    valid = {k: v for k, v in intensities.items() if v is not None}
    cleanest = min(valid, key=valid.get) if valid else None
    
    print_intensity_report(intensities, selected=cleanest)


def submit_single_job(config: Dict, async_mode: bool = False):
    """Submit a single job to the cleanest region."""
    print("\n🔍 Finding cleanest region...")
    region, intensities = pick_cleanest_region()
    print_intensity_report(intensities, selected=region)
    
    submit_func = submit_job_async if async_mode else submit_job_ssh
    log_entry = submit_func(region, config, intensities, job_index=0)
    
    # Save to log
    log_file = LOG_DIR / f"single_job_{datetime.now().strftime('%Y%m%d_%H%M%S')}.json"
    with open(log_file, 'w') as f:
        json.dump(log_entry, f, indent=2)
    
    print(f"\n📋 Log saved: {log_file}")


def test_ssh_connections():
    """Test SSH connectivity to all regions."""
    print("\n🔌 Testing SSH connections...")
    print("-" * 60)

    for region, config in REGIONS.items():
        ip = config["ip"]
        print(f"\n  Testing {region} ({ip})...")

        if "100.x.x.x" in ip:
            print(f"    ⚠️  Placeholder IP - skipping")
            continue

        ssh_cmd = [
            "ssh",
            "-i", SSH_KEY_PATH,
            "-o", "StrictHostKeyChecking=no",
            "-o", "UserKnownHostsFile=/dev/null",
            "-o", "ConnectTimeout=10",
            f"{SSH_USER}@{ip}",
            "echo 'Connection OK' && nvidia-smi --query-gpu=name,memory.total --format=csv,noheader",
        ]

        try:
            result = subprocess.run(ssh_cmd, capture_output=True, text=True, timeout=30)
            if result.returncode == 0:
                print(f"    ✅ Connected!")
                print(f"    GPU: {result.stdout.strip()}")
            else:
                print(f"    ❌ Failed: {result.stderr.strip()}")
        except subprocess.TimeoutExpired:
            print(f"    ❌ Timeout")
        except Exception as e:
            print(f"    ❌ Error: {e}")

    print("\n" + "-" * 60)


def tail_logs(region: str = None):
    """Tail logs from a specific region or show available logs."""
    if region:
        if region not in REGIONS:
            print(f"❌ Unknown region: {region}")
            print(f"   Available regions: {', '.join(REGIONS.keys())}")
            return

        region_config = REGIONS[region]
        ip = region_config["ip"]

        if "100.x.x.x" in ip:
            print(f"❌ Region {region} has placeholder IP")
            return

        print(f"\n📋 Listing logs from {region} ({ip})...")

        # First list available logs
        list_cmd = [
            "ssh",
            "-i", SSH_KEY_PATH,
            "-o", "StrictHostKeyChecking=no",
            "-o", "UserKnownHostsFile=/dev/null",
            "-o", "ConnectTimeout=10",
            f"{SSH_USER}@{ip}",
            "ls -lht ~/training/logs/*.log 2>/dev/null | head -20 || echo 'No logs found'",
        ]

        try:
            result = subprocess.run(list_cmd, capture_output=True, text=True, timeout=30)
            print(result.stdout)

            if "No logs found" not in result.stdout and result.returncode == 0:
                print(f"\n💡 To tail a specific log:")
                print(f"   ssh -i {SSH_KEY_PATH} {SSH_USER}@{ip} 'tail -f ~/training/logs/<job_id>.log'")
                print(f"\n💡 Or to tail the most recent log:")
                print(f"   ssh -i {SSH_KEY_PATH} {SSH_USER}@{ip} 'tail -f \\$(ls -t ~/training/logs/*.log | head -1)'")
        except Exception as e:
            print(f"❌ Error: {e}")
    else:
        print("\n📋 Available regions:")
        for region_name, config in REGIONS.items():
            ip = config["ip"]
            if "100.x.x.x" not in ip:
                print(f"  • {region_name:10} ({ip})")
        print(f"\nUsage: python spatial_router.py --tail-logs <region>")
        print(f"Example: python spatial_router.py --tail-logs quebec")


# ============================================================================
# Main Entry Point
# ============================================================================

def main():
    parser = argparse.ArgumentParser(
        description='Carbon-Aware Spatial Router for ML Training',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Check current carbon intensity
  python spatial_router.py --check-intensity

  # Test SSH connections to all regions
  python spatial_router.py --test-ssh

  # List available regions for log tailing
  python spatial_router.py --tail-logs

  # List logs from a specific region
  python spatial_router.py --tail-logs quebec

  # Run the full experiment (blocking mode - waits for each job)
  python spatial_router.py --run-experiment

  # Run experiment in async mode (submits and moves on)
  python spatial_router.py --run-experiment --async

  # Submit a single job to cleanest region
  python spatial_router.py --submit-single --r 16 --alpha 64 --lr 1e-4
        """
    )
    
    # Mode selection
    mode_group = parser.add_mutually_exclusive_group(required=True)
    mode_group.add_argument('--run-experiment', action='store_true',
                           help='Run the full experiment on a 4-hour schedule')
    mode_group.add_argument('--submit-single', action='store_true',
                           help='Submit a single job to the cleanest region')
    mode_group.add_argument('--check-intensity', action='store_true',
                           help='Check current carbon intensity (no job submission)')
    mode_group.add_argument('--test-ssh', action='store_true',
                           help='Test SSH connectivity to all configured regions')
    mode_group.add_argument('--tail-logs', type=str, nargs='?', const='',
                           metavar='REGION',
                           help='List or tail logs from a region (quebec, ohio, nordic)')
    
    # Job configuration (for --submit-single)
    parser.add_argument('--r', type=int, default=16, help='LoRA rank')
    parser.add_argument('--alpha', type=int, default=32, help='LoRA alpha')
    parser.add_argument('--lr', type=float, default=1e-4, help='Learning rate')
    parser.add_argument('--dropout', type=float, default=0.1, help='Dropout')
    
    # Execution mode
    parser.add_argument('--async', dest='async_mode', action='store_true',
                       help='Submit jobs asynchronously (non-blocking)')
    
    args = parser.parse_args()
    
    if args.check_intensity:
        check_intensity_only()
    elif args.test_ssh:
        test_ssh_connections()
    elif args.tail_logs is not None:
        tail_logs(args.tail_logs if args.tail_logs else None)
    elif args.submit_single:
        config = {
            'r': args.r,
            'alpha': args.alpha,
            'lr': args.lr,
            'dropout': args.dropout,
        }
        submit_single_job(config, async_mode=args.async_mode)
    elif args.run_experiment:
        run_experiment(async_mode=args.async_mode)


if __name__ == "__main__":
    main()