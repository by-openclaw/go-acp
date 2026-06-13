# ansible/secrets — interim credential store (pre-vault)

Fleet auth model (do **not** mix — it's just how ansible connects per OS):

- **Linux / macOS** → SSH with the `by-rune` key. No password lives here.
- **Windows** (`win11`, `winsrv`) → WinRM with user `by-rune` + a password.

## Usage

1. Copy the template and fill the real values (this file is **gitignored**):

   ```
   cp credentials.example.json credentials.json
   # edit credentials.json -> set the real WinRM password(s)
   ```

2. Pass it at run time so nothing secret touches the command line history or git:

   ```
   ansible-playbook -i inventory/win.ini playbooks/site.yml -e @secrets/credentials.json
   ```

   `group_vars/windows.yml` reads `ansible_user` / `ansible_password` from the
   `creds.<host>` block, so no `-e ansible_password=...` on the CLI.

## Migration to ansible-vault (later)

Same JSON structure. When vault is wired:

```
ansible-vault encrypt secrets/credentials.json
ansible-playbook ... -e @secrets/credentials.json --ask-vault-pass   # or --vault-password-file
```

Only `credentials.example.json` is committed. `credentials.json` (and any
`*.vault`) is gitignored — never commit a real credential.
