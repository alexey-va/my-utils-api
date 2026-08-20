# Rebuild the dual-exit VPN with Ansible

This playbook rebuilds one `utils` relay and two independent AWG exit hosts by
orchestrating the guarded scripts in the parent directory. It is plan-only by
default. It never downloads private keys from a host to the controller and all
tasks that touch Vault values use `no_log: true`.

Only paths listed in `stage-files.txt` are copied to managed hosts. The staging
directory is reset before each apply, so a local `vault.yml`, generated
`client.params`, AWG configs, build output, and other untracked files cannot be
distributed accidentally.

## Prepare inventory and encrypted variables

```bash
cd ops/wireguard/ansible
cp inventory.example.yml inventory.yml
cp vault.example.yml vault.yml
```

Edit the public host topology in `inventory.yml`. Generate the stable private
keys with a restrictive umask and immediately place them in `vault.yml`:

```bash
umask 077
wg genkey
awg genkey
awg genkey
ansible-vault encrypt vault.yml
```

Do not save the command output in shell history, Git, chat, or a plaintext
inventory. Keep the Vault password outside this repository. The stable
`vault_wireguard_server_private_key` is what lets existing client profiles keep
working after the relay is rebuilt.

Validate the public example shape without revealing any value:

```bash
python3 validate.py --inventory inventory.example.yml --vault-vars vault.example.yml
```

For a real encrypted Vault file, Ansible performs the same secret assertions
after decryption:

```bash
ansible-playbook --ask-vault-pass site.yml
```

That command only validates and prints the topology. It does not copy files,
install packages, modify routes, or restart services.

## Apply to fresh hosts

First ensure root SSH keys already work on all three hosts. Then run:

```bash
ansible-playbook --ask-vault-pass site.yml -e vpn_apply=true
```

The exit bootstrap changes SSH to key-only authentication. It refuses to do so
unless `/root/.ssh/authorized_keys` is already populated. Existing managed
VPN state is not overwritten. A deliberate rebuild of already managed state
requires both gates:

```bash
ansible-playbook --ask-vault-pass site.yml \
  -e vpn_apply=true \
  -e vpn_replace=true
```

Replacement rotates exit-side AWG server keys and briefly restarts affected
interfaces. The stable standard WireGuard ingress key remains the Vault value.
Do not use replacement as a health check.

## Disaster rebuild order

1. Restore the API database and `WIREGUARD_CREDENTIALS_ENCRYPTION_KEY`.
2. Provision fresh relay and exit hosts in `inventory.yml`.
3. Run the plan-only command and inspect the reported topology.
4. Apply without `vpn_replace` to the fresh hosts.
5. Verify the actual client path, DNS, RU-direct route, and selected external
   egress from a real device.
