# GitHub commit-email-bot

commit-email-bot sends emails when changes are pushed to a Git repository on Github.

## Getting started

[Install the commit-emails GitHub app](https://github.com/apps/commit-emails)

In your repo, commit a file called `.github/commit-emails.toml` that specifies the recipients and the format of the emails (the default is html, text is also supported)

```toml
to = "alice@example.com,bob@example.net"

[email]
format = "html"
```

Every email from commit-email-bot contains the string `jD27HVpTX3tELRBjcpGsK6io7` followed by the name of the repo. You can use this to easily filter commit emails in Gmail.

## Deploying

Create a Digital Ocean droplet and a volume. Clone the repo on the droplet. Mount the created volume (following Digital Ocean's instructions to get its UUID) to `/root/commit-email-bot/persist`.

You'll need to point the commit-emails.xyz domain to the droplet: configure NameCheap's A record to the droplet's public IPv4 address.

Install docker:

```bash
sudo apt install -y docker.io docker-compose docker-buildx
```

A 512MB virtual machine runs out of memory when building, but not when running, so make sure to configure some swap space:

```bash
sudo fallocate -l 2G /swapfile
sudo chmod 600 /swapfile
sudo mkswap /swapfile
sudo swapon /swapfile
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
```

Copy the private key for the production secrets in `.env.production`:

```bash
rsync .env.keys root@commit-emails.xyz:./commit-email-bot/
```

Finally, build and run the server with `dotenvx run -f .env.keys -- docker-compose up --build`.

## Future work

- Expose a branch filter option (simplifying the set of git_multimail options).
- Clean up old cloned repos to free up space.
- Use the GitHub API to fetch diffs and format them using pygments or shiki. This would avoid cloning the repo (the
  major source of state) and remove the dependency on git_multimail.py, as well as enable other features.
  - Add syntax highlight within diffs
  - Improve the linking to GitHub
  - Allow filtering emails based on file path
- Upgrade to a paid Mailgun account to support a wider audience.

## Acknowledgment

Based on David's [git-mailbot](https://github.com/davidlazar/git-mailbot).
