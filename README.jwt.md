
# Authentication

## Protecting downloads

Public metadata is safe to share, but letting anyone download files might kill your bandwidth (or your server). That's why file downloads can be protected by a simple JWT mechanism.

- If you **don't** set the `ANNA_JWT_SECRET` environment variable, authentication is disabled. Everyone can download files.
- If you **do** set a secret, clients will need a valid Bearer token to access the download endpoint.

## Generating a token

You don't need a complex auth server to generate a token. Here is a quick bash snippet to create a random secret and sign a token valid for 1 year:

```bash
# 1. Generate a random secret
export ANNA_JWT_SECRET=$(openssl rand -hex 32)
echo "ANNA_JWT_SECRET=$ANNA_JWT_SECRET"

# 2. Create a JWT valid for 1 year
header='{"alg":"HS256","typ":"JWT"}'
payload="{\"exp\":$(($(date +%s) + 31536000))}"

base64url() { echo -n "$1" | openssl base64 -e -A | tr '+/' '-_' | tr -d '='; }
sign() { echo -n "$1" | openssl dgst -sha256 -hmac "$ANNA_JWT_SECRET" -binary | openssl base64 -e -A | tr '+/' '-_' | tr -d '='; }

header_enc=$(base64url "$header")
payload_enc=$(base64url "$payload")
signature=$(sign "$header_enc.$payload_enc")

echo "Bearer Token: $header_enc.$payload_enc.$signature"
```

Use this token in your API calls: `Authorization: Bearer <your-token>`.
