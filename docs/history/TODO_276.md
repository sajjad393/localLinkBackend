# TODO 276 - "My Android Phone (xxxx)" never shown as a name (web + app)

Status: PROPOSAL ONLY (freeze ON). Build only after the user says "Build".
User (14 Nov, screenshot image-5, web Conversations): "My Android Phone(xyz) - i ask many time dont show
this any where in web + app; if display name not available show user name without @".
Code: backend `86aa525`, app `6bf30bd`.

## Rule (one rule everywhere)
- Person name = display name, but NOT when it is a name the phone gave itself
  ("My Android Phone (...)", "My LinkUp phone", "Phone", "LinkUp user", empty) -> username WITHOUT @
  -> "LinkUp user" (only when there is no username at all).
- The small grey line under a name keeps "@username" (user choice: keep_handle).
- A PHONE label (Sessions, Network "Phone" column, map tooltip, audit "· phone") = the phone's real custom
  name, or "<person>'s phone" when it is a default name (user choice: owner_phone). Never "My Android Phone (xxxx)".

## Root cause (code, confirmed)
1. The app names every phone `My Android Phone (<last 8 of id>)` (`authentication_repository.dart:41-45`).
2. The SERVER copies that phone name into the person's profile name on register / first login:
   `auth.go:175` and `auth.go:395` (`INSERT INTO profiles(... display_name ...) VALUES(?, deviceName ...)`).
   So noshad's profile name IS "My Android Phone (d5d181c0)". (`ensureProfile`, profile.go:45-52, already
   uses the username - only these two spots use the phone name.)
3. The server has no guard (no "default phone name" check anywhere in Go / web JS).
   The app has one (`phone_label_255.dart`, `display_label.dart`), the web has none.
4. Web Conversations names come from `deviceLabels` (`admin_conversations.go:155`) = phone name only, no
   profile; thread sender names too (`:344`, `:447`).
5. Group member names (sent to the APP too): `memberNameSQL` (`groups_admin.go:56`), `groups.go:270`,
   `sync.go:153`, `groups_g6.go:156/369` = profile name, else phone name.

## Fix - backend (zip first)
S-1 New `name_276.go`: `isDefaultPhoneName276(s)` (same list as the app 255 rule), `personName276(display,
    username)`, `phoneLabel276(phoneName, person)`.
S-2 Source: `auth.go:175/395` create the profile with the USERNAME, not the phone name.
S-3 Guard: profile save (`profile.go` update) turns a default phone name into the username (old apps can
    not put it back).
S-4 One-time cleanup at start (idempotent, logged `NAME_FIX_276 n=`): profiles whose display name is a
    default phone name -> display name = username. Nothing else changes; a real name a user typed stays.
S-5 Names in API answers use S-1:
    - `admin_conversations.go` deviceLabels + thread sender (join users + profiles).
    - `memberNameSQL` + `groups.go:270`: display -> username -> 'LinkUp user' (join users) - fixes group
      member names in the app too.
    - Admin answers with `device_name` (sessions `admin_panel.go:252`, link map / phones216
      `admin_216.go:108,524`, audit / security events, recovery `target_device_name`, `deviceUser`
      `admin_actions_220.go:167`, `duplicate_235.go:62`) -> `phoneLabel276`.
S-6 Web `adminweb`: one helper `ui.name276(o)`; remove every `|| n.device_name` / `|| d.device_name`
    person fallback (`views.js:880, 1314, 1347`), Network "Phone" column (`:903`), map tooltip (`:1316`),
    audit "who" uses name first not "@user" (`:998-1000`), Conversations `shownName` (`:1786`), chat
    author (`:1692, :1967`), sessions Phone column (`:834`), "New owner" text (`:626, :654`).

## Fix - app (3.0.85 zip)
A-1 Own name fallbacks that use the phone name -> username -> "LinkUp user":
    `messenger_settings_screen.dart:180`, `account_center_screen.dart:180`,
    `account_switch_sheet.dart:46`, `group_nearby_store.dart:148` (this one is SENT to group members
    offline - so others saw it).
A-2 Admin panel: `admin_sessions_screen.dart:96`, `admin_security_screen.dart:33`, recovery `:148/:219`
    -> `phoneTitle255`; `admin_people216.dart:135` no phone-name fallback.
A-3 Names saved earlier on phones (contacts / group members cached as "My Android Phone (...)") are
    replaced on show by the 255 rule and refreshed from the fixed server profile.
A-4 The phone itself keeps its internal name (used for login only) - never shown.

## Tests
- Old-test changes (listed): `profile_test.go:38` DisplayName "Phone" -> "profileuser"; `admin_216_fields_test.go:150` callee_name "dev-bilal-216" -> "bilal216". Both asserted the old bug (phone name saved as the person's name at register, S-2).
- Go `name_276_test.go`: register -> profile name = username; profile save with "My Android Phone (x)"
  -> username; cleanup fixes old rows, keeps a real name; admin conversations / thread / group members /
  sessions JSON contain no "My Android Phone".
- Go `admin_web_276_test.go`: views.js has no `|| n.device_name` person fallback; `ui.name276` exists.
- Flutter `test/bundle276_names_test.dart`: the 4 own-name fallbacks + 3 admin screens never show
  "My Android Phone"; username shown without @.

## Not changed
- The grey "@username" line. Login, phone id, internal phone name.
