# GitHub commit-email-bot

commit-email-bot sends emails when changes are pushed to a Git repository on Github.

## Getting started

[Install the commit-emails GitHub app](https://github.com/apps/commit-emails)

In your repo, commit a file called `.github/commit-emails.json` that specifies the recipients and the format of the emails (html or text):

```
{
  "commitEmailFormat": "html",
  "commitList": "alice@example.com,bob@example.net"
}
```

Every email from commit-emails-bot contains the string `jD27HVpTX3tELRBjcpGsK6io7` followed by the name of the repo. You can use this to easily filter commit emails in Gmail.

## Deploying

Use `docker compose up --build`.

## Acknowledgment

Based on David's [git-mailbot](https://github.com/davidlazar/git-mailbot).
