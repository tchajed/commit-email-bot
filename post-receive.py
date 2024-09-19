#! /usr/bin/env python3

import sys
import json
import git_multimail

git_multimail.REFCHANGE_INTRO_TEMPLATE = ""
git_multimail.REVISION_INTRO_TEMPLATE = ""
git_multimail.COMBINED_INTRO_TEMPLATE = ""

# Include a random string in all emails to make it easy to filter in gmail.
git_multimail.FOOTER_TEMPLATE = """\

-- \n\
git mailbot 17HFp8KmxqrjXDu3BDa6oRqAGxK1w6WFrE %(repo_shortname)s
"""

git_multimail.REVISION_FOOTER_TEMPLATE = git_multimail.FOOTER_TEMPLATE
git_multimail.COMBINED_FOOTER_TEMPLATE = git_multimail.FOOTER_TEMPLATE

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
X-Git-Host: %(fqdn)s
X-Git-Repo: %(repo_shortname)s
X-Git-Refname: %(refname)s
X-Git-Reftype: %(refname_type)s
X-Git-Rev: %(rev)s
X-Git-NotificationType: diff
X-Git-Multimail-Version: %(multimail_version)s
Auto-Submitted: auto-generated
"""

class ExternalConfigEnvironmentMixin(git_multimail.Environment):
    """Sets some parameters from an external config."""

    def __init__(self, external_config, **kw):
        super(ExternalConfigEnvironmentMixin, self).__init__(**kw)
        self.__reponame = external_config.get('repoName')
        self.__commitlist = external_config.get('commitList')
        self.commit_email_format = external_config.get('commitEmailFormat', 'text')
        self.from_commit = 'author'
        self.from_refchange = 'pusher'
        self.date_substitute = ''

    def get_revision_recipients(self, revision):
        if self.__commitlist is None:
            return super(ExternalConfigEnvironmentMixin,
                         self).get_revision_recipients(revision)
        return self.__commitlist
    
    def get_repo_shortname(self):
        if self.__reponame is None:
            return super(ExternalConfigEnvironmentMixin,
                         self).get_repo_shortname()
        return self.__reponame

    def get_emailprefix(self):
      return self.get_repo_shortname() + ' '


# List of mixins based on build_environment_klass.
mailbox_mixins = [git_multimail.GenericEnvironmentMixin, ExternalConfigEnvironmentMixin] + git_multimail.COMMON_ENVIRONMENT_MIXINS + [git_multimail.Environment]

MailbotEnvironment = type('MailbotEnvironment', tuple(mailbox_mixins), {})

config = git_multimail.Config('multimailhook')

json_data = git_multimail.read_git_output(["show", "HEAD:.github/mailbot.json"])
external_config = json.loads(json_data)

try:
    environment = MailbotEnvironment(config=config, external_config=external_config)
except git_multimail.ConfigurationException:
    sys.stderr.write('*** %s\n' % sys.exc_info()[1])
    sys.exit(1)


# Choose the method of sending emails based on the git config:
mailer = git_multimail.choose_mailer(config, environment)

# Read changes from stdin and send notification emails:
git_multimail.run_as_post_receive_hook(environment, mailer)
