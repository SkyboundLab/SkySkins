## SkySkins

A simple API for rendering avatars from Mojang and Ely.

### Routes

- `/m/{id}` - Fetches a avatar from Mojang
- `/e/{name}` - Fetches a avatar from Ely
- `/a/{id}/{name}` - Fetches a avatar from either Mojang or Ely

### Caching

The API caches avatars for 48 hours.

### Environment Variables

- `REDIS_ADDR` - Redis address
- `REDIS_PASSWORD` - Redis password
- `REDIS_DB` - Redis database
- `PORT` - Port to listen on