package main

func shortUsage() string {
	return `Usage: restic-webctl [global flags] <command> [args]

Commands:
  auth       setup, passwd, status
  status     app health summary (dashboard)
  activity   live runs + recent history
  folder     manage backup folders
  database   manage database sources (dumped via Docker)
  docker     discover Coolify/Docker sites, volumes and databases
  repo       manage storage repositories
  job        manage jobs (run backups, schedules, retention, forget)
  run        inspect, follow, stop, and download runs

Global flags:
  --database URL         Postgres URL (env DATABASE_URL)
  --data DIR             data directory (env RESTIC_WEB_DATA; default /app/data or data)
  --json                 machine-readable JSON on stdout
  --timeout D            limit for repo test/unlock/snapshots, job snapshots
                         (default 10m; e.g. 90s, 30m; 0 = no limit)
  --quiet, -q            less human chatter
  --help, -h             show help

See docs/cli.md for the full guide. Use "restic-webctl <command> --help" for details.
`
}

func fullUsage() string {
	return shortUsage() + `
Examples:
  restic-webctl auth setup --password '…'
  restic-webctl --json status
  restic-webctl folder create --name home --path /data/home
  restic-webctl repo create --name local --backend Local --path /data/repo --password '…'
  restic-webctl job create --name nightly --folder home --repo local
  restic-webctl job run nightly --wait
  restic-webctl repo check local --read-data-subset 10% --json
  restic-webctl run watch <runId>
  restic-webctl activity --json

Coolify / Docker:
  docker exec <container> restic-webctl --json status
  restic-webctl docker resources
  restic-webctl database create --name shop-db --container <db-container> --db shop
  restic-webctl job create --name shop-db --db-source shop-db --repo s3
  restic-webctl repo restore-db s3 --snapshot ID --db-source shop-db --into shop_restored
`
}

func helpAuth() string {
	return `Usage:
  restic-webctl auth status
  restic-webctl auth setup [--password PASS]
  restic-webctl auth passwd --current CUR --new NEW
`
}

func helpFolder() string {
	return `Usage:
  restic-webctl folder list
  restic-webctl folder get <id|name>
  restic-webctl folder create --name NAME --path PATH
  restic-webctl folder update <id|name> [--name NAME] [--path PATH]
  restic-webctl folder delete <id|name>
`
}

func helpRepo() string {
	return `Usage:
  restic-webctl repo list
  restic-webctl repo get <id|name>
  restic-webctl repo create --name NAME --backend Local|S3 --password PASS [options]
  restic-webctl repo update <id|name> [options]
  restic-webctl repo delete <id|name>
  restic-webctl repo test <id|name>
  restic-webctl repo init <id|name> [--wait]
  restic-webctl repo unlock <id|name>
  restic-webctl repo check <id|name> [--read-data | --read-data-subset S] [--wait] [--follow]
  restic-webctl repo speedtest <id|name> [--size MB] [--wait] [--follow]
  restic-webctl repo snapshots <id|name>
  restic-webctl repo files <id|name> --snapshot ID [--path DIR]
  restic-webctl repo restore <id|name> --snapshot ID --target PATH [--include P]… [--stop-containers] [--wait]
  restic-webctl repo download <id|name> --snapshot ID [--include P]… [--wait]
  restic-webctl repo restore-db <id|name> --snapshot ID --db-source ID|NAME [--into NAME] [--no-safety-backup] [--wait]
  restic-webctl repo forget <id|name> --snapshot ID [--wait]
  restic-webctl repo reset <id|name> [--wait]

Local create options:  --path DIR
S3 create options:     --endpoint URL --bucket NAME [--region R] --access-key K --secret-key S

check verifies repository structure; --read-data also reads every data pack,
--read-data-subset reads part of them (e.g. 10%, 1/5, 500M).

speedtest backs up --size MB (default 64) of random data and reads it back,
then scores the repository 0-100 (summary.speed). The test snapshots are
forgotten; their space is reclaimed at the next prune.

restore replaces the destination (files not in the snapshot are deleted). With
--include only the given snapshot paths are restored and replaced.
restore-db drops the database and recreates it from the dump; it first takes a
safety snapshot of the current database and puts it back if the import fails.
`
}

func helpJob() string {
	return `Usage:
  restic-webctl job list
  restic-webctl job get <id|name>
  restic-webctl job create --name NAME --folder ID|NAME|--db-source ID|NAME --repo ID|NAME [schedule] [retention]
  restic-webctl job update <id|name> [fields…]
  restic-webctl job delete <id|name> [--forget]
  restic-webctl job run <id|name> [--wait] [--follow]
  restic-webctl job retention <id|name> [--dry-run] [--wait] [--follow]
  restic-webctl job forget <id|name> [--delete-job] [--wait] [--follow]
  restic-webctl job runs <id|name> [--limit N]
  restic-webctl job snapshots <id|name>

Schedule flags:
  --schedule-enabled / --schedule-disabled
  --schedule-kind hourly|every|daily|weekly
  --every 6h   --schedule-at HH:MM   --weekdays 0,1,2 (0=Sun)

Retention flags:
  --retention-enabled / --retention-disabled
  --retention-preset light|balanced|long|custom
  --keep-last N --keep-hourly N --keep-daily N --keep-weekly N --keep-monthly N --keep-within-days N
`
}

func helpDatabase() string {
	return `Usage:
  restic-webctl database list
  restic-webctl database get <id|name>
  restic-webctl database create --name NAME --container CONTAINER --db DB [--engine postgres|mysql|mariadb]
  restic-webctl database update <id|name> [--name NAME] [--db DB]
  restic-webctl database delete <id|name>

A database source is one database inside a PostgreSQL, MySQL or MariaDB
container. It is dumped and restored inside that container (docker exec), with
the credentials from the container's own environment.
`
}

func helpDocker() string {
	return `Usage:
  restic-webctl docker status
  restic-webctl docker resources
  restic-webctl docker databases <container>

Needs the Docker socket (/var/run/docker.sock or DOCKER_HOST). On Coolify,
resources are the applications, services and databases of the dashboard.
`
}

func helpRun() string {
	return `Usage:
  restic-webctl run list [--status active|finished|STATUS] [--kind KIND] [--job ID|NAME] [--limit N]
  restic-webctl run get <runId>
  restic-webctl run log <runId> [--after N] [--follow]
  restic-webctl run watch <runId>
  restic-webctl run stop <runId>
  restic-webctl run download <runId> -o FILE.zip
`
}

func helpActivity() string {
	return `Usage:
  restic-webctl activity [--limit N]
`
}

func helpStatus() string {
	return `Usage:
  restic-webctl status
`
}
