## SkySkins

A simple API for rendering avatars from Drasl, Mojang and Ely.

### Routes

- `/d/{id}` - Fetches a avatar from Drasl
- `/m/{id}` - Fetches a avatar from Mojang
- `/e/{name}` - Fetches a avatar from Ely
- `/a/{id}/{name}` - Fetches a avatar from either Drasl, Mojang or Ely
- `/textures/signed/{id}` - Fetches a signed texture from Drasl, Mojang or Ely

### Caching

The API caches avatars for 48 hours.

### Environment Variables

- `PORT` - Port to listen on

- `MONGODB_URI` - MongoDB URI

- `REDIS_ADDR` - Redis address
- `REDIS_PASSWORD` - Redis password
- `REDIS_DB` - Redis database

- `DRASL_TOKEN` - Drasl authentication token
- `DRASL_URL` - Drasl API URL