# gostudentubl

Automated attendance runner for Moodle-based courses with scheduled execution.

## Local run

1. Copy environment template:

```bash
cp .env.example .env
```

2. Fill required values in `.env`.
3. Run locally:

```bash
make run
```

## Docker agent deployment

This project can run as a long-lived Docker service (`attendance-agent`) that executes scheduled jobs internally.

### Deploy

```bash
make docker-deploy
```

Equivalent command:

```bash
docker compose up -d --build
```

### Cross-project WhatsApp integration (same host)

If `wa-bot-notif` runs as a separate Docker project on the same machine, use a shared external network.

1. Create shared network once:

```bash
docker network create homelab_integration
```

2. Set in `.env`:

```env
INTEGRATION_NETWORK=homelab_integration
WA_ENDPOINT=http://wa-bot-notif-api:5000/send
```

If wa-bot-notif uses a non-default `PORT` (for example `5001`), match it in `WA_ENDPOINT`.

3. Start `wa-bot-notif` stack first, then this stack.

Do not use `localhost` for cross-container calls; use service/container DNS on the shared network.

### Operate

- Tail logs:

```bash
make docker-logs
```

- Restart:

```bash
make docker-restart
```

- Stop:

```bash
make docker-down
```

### Trigger manual run in container

The app listens to `SIGUSR1` and executes a direct attendance run.

```bash
make docker-signal-run
```

Equivalent command:

```bash
docker compose kill -s SIGUSR1 attendance-agent
```

## Notes

- Scheduler timezone is controlled by `TIMEZONE`.
- App logs go to stdout and rotating `app.log` inside container workdir (`/app`).
- Keep secrets only in `.env` (already ignored by git).
- Run a single agent instance unless you add distributed locking, otherwise duplicate attendance jobs can occur.

## Periode Mode

This project supports dynamic period filtering so monthly manual config updates are optional.

- `PERIODE_MODE=auto` (recommended): accepts current month and next month (`MMYY`) automatically.
  - Example in Feb 2026: `0226` and `0326` are accepted.
- `PERIODE_MODE=manual`: accepts only values in `ALLOWED_PERIODES` (comma separated).
- `PERIODE_MODE=legacy`: keeps strict `CURRENT_PERIODE` equality behavior.

Additional safety setting:

- `MAX_COURSES_PER_RUN`: hard cap to prevent accidental over-selection.
  - `0` disables the cap (default behavior).
