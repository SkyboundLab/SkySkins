## SkySkins

A simple API for rendering avatars from Mojang and Ely.

### Routes

- `/d/{id}` - Fetches a avatar from Drasl
- `/m/{id}` - Fetches a avatar from Mojang
- `/e/{name}` - Fetches a avatar from Ely
- `/a/{id}/{name}` - Fetches a avatar from either Mojang or Ely

### Caching

The API caches avatars for 48 hours.

### Environment Variables

- `PORT` - Port to listen on

- `REDIS_ADDR` - Redis address
- `REDIS_PASSWORD` - Redis password
- `REDIS_DB` - Redis database

- `DRASL_TOKEN` - Drasl authentication token
- `DRASL_URL` - Drasl API URL