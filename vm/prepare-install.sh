#!/bin/bash
# Host side, step 1: stage the Ubuntu autoinstall for the "onion" VM.
# Extracts the installer kernel/initrd, builds the NoCloud seed ISO (user,
# SSH key, network, firewall), and points the VM at them. Idempotent.
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
BOOT=/var/lib/libvirt/boot
ISO=$BOOT/ubuntu-24.04.5-live-server-amd64.iso
KEY=$HOME/.ssh/onion_ed25519.pub
PWFILE=$HOME/.ssh/onion-console-password   # emergency console login for user ramon

[ -f "$ISO" ] || { echo "missing $ISO"; exit 1; }
[ -f "$KEY" ] || ssh-keygen -q -t ed25519 -N '' -C "$USER@$(hostname)->onion-vm" -f "${KEY%.pub}"
if [ ! -f "$PWFILE" ]; then
  (umask 077; openssl rand -base64 18 > "$PWFILE")
fi

work=$(mktemp -d); trap 'rm -rf "$work"' EXIT
HASH=$(openssl passwd -6 -stdin < "$PWFILE") SSHKEY=$(cat "$KEY") \
  python3 - "$HERE/autoinstall/user-data.tmpl" "$work/user-data" <<'EOF'
import os, sys
s = open(sys.argv[1]).read()
s = s.replace("__PASSWORD_HASH__", os.environ["HASH"]).replace("__SSH_KEY__", os.environ["SSHKEY"])
open(sys.argv[2], "w").write(s)
EOF
printf 'instance-id: onion-%s\nlocal-hostname: onion\n' "$(date +%s)" > "$work/meta-data"
sudo cloud-localds "$BOOT/onion-seed.iso" "$work/user-data" "$work/meta-data"

mnt=$(mktemp -d)
sudo mount -o loop,ro "$ISO" "$mnt"
sudo cp "$mnt/casper/vmlinuz" "$BOOT/install-vmlinuz"
sudo cp "$mnt/casper/initrd" "$BOOT/install-initrd"
sudo umount "$mnt"; rmdir "$mnt"

sudo virsh dumpxml --inactive onion > "$work/dom.xml"
python3 - "$work/dom.xml" <<'EOF'
import sys, xml.etree.ElementTree as ET
p = sys.argv[1]; t = ET.parse(p); r = t.getroot()
os_ = r.find("os")
for tag, val in (("kernel", "/var/lib/libvirt/boot/install-vmlinuz"),
                 ("initrd", "/var/lib/libvirt/boot/install-initrd"),
                 ("cmdline", "autoinstall console=ttyS0,115200n8 ---")):
    e = os_.find(tag)
    if e is None:
        e = ET.SubElement(os_, tag)
    e.text = val
r.find("on_reboot").text = "destroy"   # the installer's final reboot powers off
devs = r.find("devices")
for d in devs.findall("disk"):
    if d.get("device") == "cdrom":
        devs.remove(d)
for iso, dev in (("/var/lib/libvirt/boot/ubuntu-24.04.5-live-server-amd64.iso", "sda"),
                 ("/var/lib/libvirt/boot/onion-seed.iso", "sdb")):
    d = ET.SubElement(devs, "disk", type="file", device="cdrom")
    ET.SubElement(d, "driver", name="qemu", type="raw")
    ET.SubElement(d, "source", file=iso)
    ET.SubElement(d, "target", dev=dev, bus="sata")
    ET.SubElement(d, "readonly")
t.write(p)
EOF
sudo virsh define "$work/dom.xml" >/dev/null
echo "Staged. Start the install with:  sudo virsh start onion --console"
