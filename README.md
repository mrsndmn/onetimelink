![gjfy](https://raw.githubusercontent.com/sstark/gjfy/master/fileio/logo.png)

one time link server
====================

> [!WARNING]
> Changes in development (master) version:

- Next release 2.0 will be reworked in several ways. Main visible change is
that more functionality will be added to the gjfy binary itself, e. g. posting
secrets. Other additions are planned.
- A recent go release is needed to build.
- Existing functionality has been moved into `server` subcommand. Just use
`gjfy server` wherever you called just `gjfy` before
- Flags have been changed to be posix compatible (-> `--` instead of `-`). Just
add another `-` in front of every option. You may now use short options too,
see help.
- You should be able to build and use the master branch if you follow above
notes, but it has not been tested a lot yet.

What does it do?
----------------

gjfy is a single binary, standalone web server with only one purpose: Create
links that automatically disappear once clicked. On first click it will show a
"secret", for instance a password that somebody wants to send to someone.

The idea is that if the original receiver finds the link invalid, they know
that the secret was intercepted by a third party and the sender can reset the
password. This does not protect against eavesdropping attacks, for this you
need a TLS connection.

There is no persistency: If the server process ends, all secrets are gone.

Please be careful: Using a tool like gjfy is only advised when all other
options are even less secure (mail, e-mail, phone). In any case, if you send a
password, the receiver should be told to change it as soon as possible.

What makes it different?
------------------------

There are other tools available that do similar things. However, usually those
involve installing lots of dependencies or web frameworks and often require
setting up a database. Some of them are even offering a hosted service, so
you would be handing your secrets to a third party.

Gjfy does not need any of this: it is a completely self-contained and
on-premise system.

Probably the most notable difference is that secrets are only kept in memory.
They are never written into a database or a file. So it can never happen that,
because of a program bug or sysadmin mistake, the secrets are left on the disk.
However, it is possible that the operating system will write part of the
program memory into swap temporarily, which is not easy to avoid.

The author believes that tools like this should not load assets from external
sources and also that no javascript should be used. Gjfy will never do that and
instead try to be as simple and privacy respecting as possible.


Features
--------

  - Everything in a single binary
  - No web server or application server needed
  - No database needed
  - No persistence
  - No javascript
  - Simple json API (demo client included)
  - Simple html user interface
  - The CSS styling, logo and user message can be customised
  - Simple token based authentication
  - No subprocesses, no outbound connections

Building
--------

A precompiled binary is provided with each release. It is also easy to build
gjfy yourself, in case you prefer that:

If you do not have a go environment installed already, install it from your
linux distribution repository (e. g. `apt-get install golang-go`) or download
it from the [go home page](https://golang.org/dl/).

Download the code and run `make`, it will create a single binary file for
easy deployment.

Installation
------------

Create a directory, e. g. `/usr/local/gjfy`. Then copy the following files to it:

  - gjfy (the binary you just built)<sup>1</sup>
  - auth.db

For integration into the various system management environments like upstart or
systemd, check the init/ subdirectory for examples.

<sup>1</sup>If you installed a version <=1.2 using `go get`, the binary will be
located at `$GOPATH/bin/gjfy`, while the rest of the files will be under
`$GOPATH/src/github.com/sstark/gjfy`

Running
-------

### Subcommand `server`

Choose the IP address and port gjfy listens on with the `-listen` parameter.

Examples:

    gjfy server --listen '0.0.0.0:1234'    # listen on all IPv4 addresses
    gjfy server --listen '[::1]:4123'      # listen on localhost, IPv6 only
    gjfy server --listen ':6234'           # listen on all addresses, IPv4 and IPv6

To tell gjfy its name as seen by users of the service, use the `-urlbase` parameter like so:

    gjfy server --urlbase 'https://gjfy.example.org'
    gjfy server --urlbase 'https://gjfy.example.org:4123'

To use TLS security add the `-tls` switch:

    gjfy server --tls

The scheme will automatically switch to https unless you set urlbase. Before
you can turn on tls you must create a certificate file called `gjfy.crt` and a
key file called `gjfy.key`. TLS versions below 1.2 are refused.

To bound how much memory the store may take, use `-max-entries` (default
10000). Once the store is full, creating a secret returns 503 rather than
letting the process grow without limit:

    gjfy server --max-entries 500

Use `gjfy server --help` for help.

### Subcommand `completion`

Removed in this fork along with cobra, see Hardening. gjfy is started by an
init system, not typed at a prompt.

Options
-------

Custom CSS styling can by applied by placing a file "custom.css" in either
`/etc/gjfy/custom.css` or `$PWD/custom.css`.

An authentication token database should placed in either `/etc/gjfy/auth.db` or
`$PWD/auth.db`. An example file is distributed with the software. New secrets
can only be created with a valid auth token in the POST request.

If you are using TLS mode you need to put in place either `/etc/gjfy/gjfy.crt`
or `$PWD/gjfy.crt`. Same applies to the key file `gjfy.key`.

The logo.png can be replaced by a custom logo if needed. (It must be png)

You may create a file `userMessageView.txt` that will contain the message the
user sees when clicking on the link. It will replace the default message. HTML
can not be used.

`$PWD/<file>` will take precedence over `/etc/gjfy/<file>` for above options.

To trigger reloading of auth.db, logo.png, custom.css or userMessageView.txt
you can send SIGHUP to the gjfy process. The TLS certificate or key won't be
reloaded this way.

Authentication
--------------

gjfy has a very simple authentication model. Requests that add tokens are
required to carry an *auth_token* in their json data. This *auth_token* is
looked up in the file `auth.db` and the corresponding email address used for
further processing and notification. If gjfy does not find the provided
auth_token, it will reject the request.

Authentication is only for adding new secrets. It does not give access to the
secrets itself.

This authentication model has some downsides and should probably be replaced by
something better. For now just keep in mind that every user in auth.db needs to
have an individual auth_token, because it is used to identify the "user".

To add an account to `auth.db`, simply edit it using your favorite editor and
add a section to the json list that is contained in it, like this:

    {
        "token": "thesecretauthtoken",
        "email": "test@example.org"
    }

Tokens must be at least 16 characters long and the email address must be a
parseable address that does not begin with `-`. If any entry fails these
checks the whole database is refused, and gjfy will reject every request until
it is fixed — it fails closed rather than running unauthenticated.

Afterwards send gjfy a hangup signal (`killall -HUP gjfy`) to make it reload
the file. In the logfile you will be informed about success or failure.

Usage
-----

Currently the only way to create new secrets is by using the json API. An
example client (gjfy-post) is included. A basic request looks like this:

    {"auth_token":"g4uhg3iu4h5i3u4","secret":"someSecret"}

By sending this to `/api/v1/new` you create a new URL. Its id is 32 random
bytes drawn from the system CSPRNG and carries no information about the secret,
so it can neither be guessed nor used to confirm a guessed secret. The reply
from the server will tell you this link in both, a
user friendly version and in an API version. Invocation of that link will
immediately lead to deletion of the secret in the server. However, there is an
exception: you can post a `"max_clicks":n` variable along with the json and it
will allow up to `n` clicks.

The authentication token sent with the request will not be stored in the
server. Instead, the associated email address will be stored with the secret,
so it can be used for email notifications (see below).

A timeout can be set by including `"valid_for:n"` in the request. The secret
will become invalid after n days, even if not clicked. The default timeout is 7
days.

Email notifications
-------------------

Removed in this fork. Upstream gjfy could mail the creator when a link was
used, by piping a message into the `mail` command with the `-notify` flag.

That was the only place gjfy started an external process, and the message body
carried client-controlled data (User-Agent, proxy headers). Dropping the
feature removes the whole class of problems that comes with it — argument
injection through the recipient, unsanitised input reaching a mail agent, and
one forked process per click under load — and leaves the server with no
subprocesses and no outbound connections at all. A test enforces this.

The `email` field in `auth.db` is still required: it identifies the account
that created a secret and is shown in the metadata view.

gjfy-post
---------

gjfy-post is a demonstration client using bash, curl and jq.

    usage: ./gjfy-post <authtoken> <secret> [maxclicks]

Required arguments are authtoken and the secret itself. Please note that
providing the secret this way makes it readable in the system process listing!

The client can be downloaded from the running server by using the URL

    /gjfy-post

Which is also linked from the root page ("/").

You can change the default URL for gjfy-post by setting the environment
variable `GJFY_POSTURL`. If you downloaded gify-post via the URL, it will
have the correct URL already configured in the script.

Hardening
---------

This fork fixes a set of issues found while reviewing the upstream code before
deploying it. What changed, and why:

  - **The secret store is synchronised.** It used to be a bare map written to
    by every request handler *and* by the expiry goroutine. That is a data
    race; the Go runtime aborts the process with `fatal error: concurrent map
    writes`, and because nothing is persisted, every outstanding secret dies
    with it. Confirmed under `-race` on the original code.
  - **Fetching a secret is atomic.** Looking an entry up and counting the click
    were two separate steps, so racing requests could each read a
    `max_clicks:1` secret before any of them deleted it.
  - **Ids come from `crypto/rand`.** They used to be a SHA-256 over the stored
    entry, which made the id a function of the secret.
  - **Secret ids are never logged.** Creation logged the id in full and the
    access log contained it for `/api/v1/get/<id>`. The id is the only
    credential for a secret, so anyone able to read the journal could read
    every secret. Logs now carry a short prefix only.
  - **The metadata view `/i` requires an auth token.** It exposes the creator's
    address and the click count without consuming a click, so it could be used
    to probe an intercepted link silently.
  - **Security headers are set**, notably `Cache-Control: no-store` (the page
    showing a secret was cacheable) and `Referrer-Policy: no-referrer` (the id
    sits in the query string).
  - **The built-in `test` secret is gone.** Every instance used to serve a
    known secret at `/g?id=test`.
  - **The HTTP server has timeouts** and a header size limit; there were none,
    which makes slowloris trivial.
  - **The store is capped** (`-max-entries`, default 10000) and single secrets
    are size limited, so creating secrets cannot exhaust memory.
  - **Creation and retrieval are rate limited** per peer address.
  - **Auth tokens are compared in constant time**, over the whole database.
  - **Email notifications are gone**, and with them the only subprocess gjfy
    ever started. See the section above.
  - **Runtime config is swapped atomically.** The SIGHUP handler reassigned
    globals that handlers were reading concurrently.
  - **No dependencies at all.** `go.mod` has no `require` directive and
    `go.sum` is empty. Upstream compiled cobra and pflag — about 12k lines of
    third party code, against 1.6k of gjfy's own — in order to parse six flags;
    for a process holding plaintext secrets in memory, that is supply chain
    surface bought for nothing. Replaced by `flag` from the standard library.
    The `completion` subcommand went with it, and the `bou.ke/monkey` test
    dependency was dropped earlier. A test keeps `go.mod` empty.

What has *not* changed: secrets are still held in memory only and in
plaintext, so the server can read them, and a restart still discards every
outstanding link. If you need the server not to be able to read secrets, use
something that encrypts in the browser instead.

FAQ
---

Q: How do you pronounce gjfy?

A: It is pronounced like "jiffy".
