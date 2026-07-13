## SkySkins

A simple API for rendering avatars from Drasl, Mojang and Ely.

### Routes

- `/d/{id}`: Fetches a avatar from Drasl.
- `/m/{id}`: Fetches a avatar from Mojang.
- `/e/{id}`: Fetches a avatar from Ely.
- `/a/{id}`: Fetches a avatar from either Drasl, Mojang or Ely.
- `/textures/signed/{id}`: Fetches a signed texture from Drasl, Mojang or Ely.

### Caching

The API caches avatars for 48 hours.

### Secrets Management

Secrets are resolved in order of precedence:

1. **Environment variables**
2. **`.env` file**
3. **[Infisical](https://infisical.com)**

When Infisical is configured, secrets are fetched on startup.

| Variable | Description | Default |
|---|---|---|
| `INFISICAL_CLIENT_ID` | Machine Identity client ID | — |
| `INFISICAL_CLIENT_SECRET` | Machine Identity client secret | — |
| `INFISICAL_PROJECT_ID` | Infisical project ID | — |
| `INFISICAL_ENVIRONMENT` | Environment slug | `prod` |
| `INFISICAL_SITE_URL` | Self-hosted URL | `https://app.infisical.com` |

### Environment Variables

| Variable | Description | Default |
|---|---|---|
| `PORT` | Port to listen on | `8080` |
| `DATABASE_URL` | PostgreSQL connection string | — |
| `DRASL_TOKEN` | Drasl authentication token | — |
| `DRASL_URL` | Drasl API URL | — |
| `MINESKIN_TOKEN` | MineSkin authentication token | — |
