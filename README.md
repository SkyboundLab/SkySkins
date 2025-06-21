## SkySkins

SkySkins is a small web server that shows Minecraft player faces.

It works with:

* **Mojang accounts** (using UUID)
* **Ely.by accounts** (using username)

It also saves results in Redis so it’s faster next time.

---

### How to use

Run this and visit these links in your browser:

* `/r/mojang/{uuid}` → get face from Mojang
* `/r/ely/{username}` → get face from Ely.by
* `/r/all/{uuid}/{username}` → try Mojang, if it fails try Ely.by

Example:

```
http://localhost:8080/r/mojang/069a79f4-44e9-4726-a5be-fca90e38aaf5
http://localhost:8080/r/ely/Notch
http://localhost:8080/r/all/069a79f4-44e9-4726-a5be-fca90e38aaf5/Notch
```

---

### How to run it

1. Install Go and Redis
2. Put this in a `.env` file:

```
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
REDIS_DB=0
```

3. Run it:

```bash
go run .
```

That’s it!