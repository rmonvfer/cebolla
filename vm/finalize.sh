#!/bin/bash
# Host side, step 2 (after the installer has powered the VM off): remove the
# installer kernel and ISOs, boot from disk from now on, autostart the VM.
set -euo pipefail
if sudo virsh domstate onion | grep -q running; then
  echo "VM is still running; wait for the installer to finish and power off."; exit 1
fi
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT
sudo virsh dumpxml --inactive onion > "$work/dom.xml"
python3 - "$work/dom.xml" <<'EOF'
import sys, xml.etree.ElementTree as ET
p = sys.argv[1]; t = ET.parse(p); r = t.getroot()
os_ = r.find("os")
for tag in ("kernel", "initrd", "cmdline"):
    e = os_.find(tag)
    if e is not None:
        os_.remove(e)
r.find("on_reboot").text = "restart"
devs = r.find("devices")
for d in devs.findall("disk"):
    if d.get("device") == "cdrom":
        devs.remove(d)
t.write(p)
EOF
sudo virsh define "$work/dom.xml" >/dev/null
sudo virsh autostart onion >/dev/null
sudo rm -f /var/lib/libvirt/boot/install-vmlinuz /var/lib/libvirt/boot/install-initrd /var/lib/libvirt/boot/onion-seed.iso
sudo virsh start onion >/dev/null
echo "VM started. Unlock the disk:  sudo virsh console onion   (type the passphrase, then Ctrl+] to detach)"
echo "Then run:  $(dirname "$0")/deploy.sh"
