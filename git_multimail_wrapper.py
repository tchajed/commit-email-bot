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


if __name__ == "__main__":
    git_multimail.main(sys.argv[1:])
