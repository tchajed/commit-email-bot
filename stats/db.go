package stats

import (
	"database/sql"
	"github.com/google/go-github/v62/github"
	_ "github.com/mattn/go-sqlite3"
	"log/slog"
	"path/filepath"
)

type Database struct {
	conn *sql.DB
}

func New(persistPath string) (Database, error) {
	db, err := sql.Open("sqlite3", filepath.Join(persistPath, "stats.sqlite3"))
	if err != nil {
		return Database{nil}, err
	}
	_, err = db.Exec(`create table if not exists installations (
		installation_id integer not null primary key,
		last_modified timestamp not null default current_timestamp,
		account text not null,
		repository_selection boolean not null,
		num_repos integer not null
		)`)
	if err != nil {
		return Database{nil}, err
	}
	_, err = db.Exec(`create table if not exists repo_stats (
		repo_id integer not null primary key,
		repo_name text not null,
		last_push timestamp not null default current_timestamp,
		num_pushes integer not null default 0,
		num_emails integer not null default 0
		)`)
	if err != nil {
		return Database{nil}, err
	}
	return Database{conn: db}, err
}

func (db Database) AddInstallation(event *github.InstallationEvent) {
	action := event.GetAction()
	if action == "created" || action == "new_permissions_accepted" {
		selection := event.GetInstallation().GetRepositorySelection() == "selected"
		_, err := db.conn.Exec(`insert or replace into installations
	(installation_id, account, repository_selection, num_repos)
values (?, ?, ?, ?) `,
			event.GetInstallation().GetID(),
			event.GetInstallation().GetAccount().GetLogin(),
			selection,
			len(event.Repositories),
		)
		if err != nil {
			slog.Warn("stats db error", slog.String("err", err.Error()), slog.String("table", "installations"))
		}
		return
	}
	if action == "deleted" {
		_, err := db.conn.Exec(`delete from installations
where installation_id = ?`,
			event.GetInstallation().GetID(),
		)
		if err != nil {
			slog.Error("stats db error",
				slog.String("table", "installations"),
				slog.String("action", "delete"))
		}
	}
}

func (db Database) UpdateInstallation(event *github.InstallationRepositoriesEvent) {
	selection := event.GetRepositorySelection() == "selected"
	_, err := db.conn.Exec(`update installations
set last_modified = current_timestamp,
	repository_selection = ?,
	num_repos = num_repos + ?
where installation_id = ?
`,
		selection,
		len(event.RepositoriesAdded)-len(event.RepositoriesRemoved),
		event.GetInstallation().GetID(),
	)
	if err != nil {
		slog.Warn("stats db error", slog.String("err", err.Error()), slog.String("table", "installations"))
	}
}

func (db Database) AddPush(event *github.PushEvent) {
	// estimate how many new emails are sent - this doesn't directly ask
	// git_multimail.py so it might be slightly different from how the script
	// computes new commits
	new_emails := 0
	for _, commit := range event.Commits {
		if commit.GetDistinct() {
			new_emails++
		}
	}
	if new_emails > 1 {
		// one extra email for the refchange notice
		new_emails++
	}
	_, err := db.conn.Exec(`insert into repo_stats
	(repo_id, repo_name, num_emails) values (?, ?, ?)
	on conflict (repo_id) do update
	set repo_name = excluded.repo_name,
		last_push = current_timestamp,
		num_pushes= num_pushes + 1,
		num_emails = num_emails + excluded.num_emails
	`,
		event.GetRepo().GetID(),
		event.GetRepo().GetName(),
		new_emails,
	)
	if err != nil {
		slog.Warn("stats db error", slog.String("err", err.Error()), slog.String("table", "repo_stats"))
	}
}
