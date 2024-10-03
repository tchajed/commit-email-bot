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

Use `dotenvx run -f .env.keys -- docker compose up --build`. (You need the private key in `.env.keys` to access the secrets in `.env.production`.)

A 512MB virtual machine runs out of memory when building, but not when running, so make sure to configure some swap space.

## Future work

Before release:
- Create a logo.

Eventually:
- Use the GitHub API to fetch diffs and format them using pygments or shiki. This would avoid cloning the repo (the
  major source of state) and remove the dependency on git_multimail.py, as well as enable other features.
  - Add syntax highlight within diffs
  - Improve the linking to GitHub
  - Allow filtering emails based on file path
- Clean up old cloned repos to free up space.
- Upgrade to a paid Mailgun account to support a wider audience.

## Acknowledgment

Based on David's [git-mailbot](https://github.com/davidlazar/git-mailbot).
