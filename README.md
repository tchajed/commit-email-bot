# Git Mailbot

Git Mailbot sends emails when changes are pushed to a Git repository on Github.
This exists because Github's own email service is being deprecated and doesn't include diffs in the emails.

## Getting started

Configure a webhook in your repo's settings on Github:

| Field        | Value                       |
| ------------ | --------------------------- |
| Payload URL  | https://example.com/webhook |
| Content type | application/json            |
| Events       | Just the push event.        |
| Secret       | (webhook secret)            |

If your repo is private, invite the @git-mailbot user as a read-only collaborator on your repo. For now, David will have to manually approve the invitation. This process can be streamlined if there is sufficient demand.

In your repo, commit a file called `.github/mailbot.json` that specifies the recipients and the format of the emails (html or text):

```
{
  "commitEmailFormat": "html",
  "commitList": "alice@example.com,bob@example.net"
}
```

Every email from mailbot contains the string `17HFp8KmxqrjXDu3BDa6oRqAGxK1w6WFrE` followed by the name of the repo. You can use this to easily filter mailbot emails in Gmail.


## Features

Git Mailbot has some advantages over [mit-pdos/mailbot](https://github.com/mit-pdos/mailbot):

* SSL.
* Doesn't require PDOS commit access.
* Doesn't leak the names of private repos.
* Based on [git-multimail](https://github.com/git-multimail/git-multimail) which supports HTML emails.
* One email per commit.
* Easier to filter emails in Gmail.

## Future work

* Receive emails for other people's repos.
* Use deploy keys instead of the git-mailbot machine user.
* Gitlab support.

