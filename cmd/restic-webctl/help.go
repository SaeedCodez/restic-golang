package main

func shortUsage() string {
	return `Usage: restic-webctl [global flags] <command> [args]

Commands:
  auth       setup, passwd, status
  status     app health summary (dashboard)
  activity   live runs + recent history
  folder     manage backup folders
  repo       manage storage repositories
  job        manage jobs (run backups, schedules, retention, forget)
  run        inspect, follow, stop, and download runs

Global flags:
  --database URL         Postgres URL (env DATABASE_URL)
  --data DIR             data directory (env RESTIC_WEB_DATA; default /app/data or data)
  --json                 machine-readable JSON on stdout
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
  restic-webctl run watch <runId>
  restic-webctl activity --json

Coolify / Docker:
  docker exec <container> restic-webctl --json status
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
  restic-webctl repo snapshots <id|name>
  restic-webctl repo restore <id|name> --snapshot ID --target PATH [--wait]
  restic-webctl repo download <id|name> --snapshot ID [--wait]
  restic-webctl repo forget <id|name> --snapshot ID [--wait]
  restic-webctl repo reset <id|name> [--wait]

Local create options:  --path DIR
S3 create options:     --endpoint URL --bucket NAME [--region R] --access-key K --secret-key S
`
}

func helpJob() string {
	return `Usage:
  restic-webctl job list
  restic-webctl job get <id|name>
  restic-webctl job create --name NAME --folder ID|NAME --repo ID|NAME [schedule] [retention]
  restic-webctl job update <id|name> [fields…]
  restic-webctl job delete <id|name> [--forget]
  restic-webctl job run <id|name> [--wait] [--follow]
  restic-webctl job retention <id|name> [--wait] [--follow]
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
