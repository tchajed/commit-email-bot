#! /usr/bin/env python3

import git_multimail
import sys

git_multimail.REFCHANGE_INTRO_TEMPLATE = ""
git_multimail.REVISION_INTRO_TEMPLATE = ""
git_multimail.COMBINED_INTRO_TEMPLATE = ""

# Include a random string in all emails to make it easy to filter in gmail.
git_multimail.FOOTER_TEMPLATE = """\

-- \n\
commit-email-bot jD27HVpTX3tELRBjcpGsK6io7 %(repo_shortname)s
"""

git_multimail.REVISION_FOOTER_TEMPLATE = git_multimail.FOOTER_TEMPLATE
git_multimail.COMBINED_FOOTER_TEMPLATE = git_multimail.FOOTER_TEMPLATE

# customize the Subject line to exclude the "01/03" and to include the branch
# name
git_multimail.REVISION_HEADER_TEMPLATE = """\
Date: %(send_date)s
To: %(recipients)s
Cc: %(cc_recipients)s
Subject: %(emailprefix)s%(short_refname)s: %(oneline)s
MIME-Version: 1.0
Content-Type: text/%(contenttype)s; charset=%(charset)s
Content-Transfer-Encoding: 8bit
From: %(fromaddr)s
Reply-To: %(reply_to)s
In-Reply-To: %(reply_to_msgid)s
References: %(reply_to_msgid)s
Thread-Index: %(thread_index)s
X-Git-Host: %(fqdn)s
X-Git-Repo: %(repo_shortname)s
X-Git-Refname: %(refname)s
X-Git-Reftype: %(refname_type)s
X-Git-Rev: %(rev)s
X-Git-NotificationType: diff
X-Git-Multimail-Version: %(multimail_version)s
Auto-Submitted: auto-generated
"""

git_multimail.LINK_HTML_TEMPLATE = """\
<p><a href="%(browse_url)s">View this commit on GitHub</a>.</p>
"""

git_multimail.COMBINED_REFCHANGE_REVISION_SUBJECT_TEMPLATE = (
    '%(emailprefix)s %(short_refname)s: %(oneline)s'
)


if __name__ == "__main__":
    git_multimail.main(sys.argv[1:])
