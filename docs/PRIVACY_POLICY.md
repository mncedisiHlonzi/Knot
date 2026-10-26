# Knot — Privacy Policy

**Last Updated: [Date to be set at launch]**

Knot is built on a simple belief: people help other people understand human stories across language and cultural boundaries. Trust is the foundation of that belief, and trust requires transparency. This policy explains, in plain language, what data we collect, why we collect it, and how we protect it.

## 1. What We Collect

We collect only the information necessary to operate Knot. We do not collect data to sell it.

*   **Account Information:** When you register, we collect your email address, a display name, and a password. We never store your password in plain text; it is hashed with a strong, slow algorithm (Argon2id).
*   **Content You Create:** We collect the stories, versions (adaptations), comments, bridges, inquiries, and media (images/videos) you choose to share. This content is the heart of Knot, and it is linked to your account.
*   **Your Rooted Signal:** If you choose to set a Rooted signal, we collect the city or region you enter and a self-declared duration. We do **not** collect your precise location, GPS data, or street address. Rooted is a declaration, not a verification.
*   **Session Information:** For security, we record the device and approximate location (IP address) where you log in. This allows you to see your active sessions and revoke any you don't recognize.

## 2. What We Do Not Collect

*   We do not track you across the internet.
*   We do not use third-party advertising trackers or sell your data to advertisers.
*   We do not scan your private messages (Knot does not have private messages).
*   We do not collect your location unless you explicitly set a Rooted signal or enter an approximate location for a story.

## 3. How We Use Your Data

*   To operate Knot and provide the features you use.
*   To secure your account (authentication, session management, rate limiting).
*   To send you essential emails (email verification, password resets).
*   To enforce our Terms of Service (moderation, blocking).

## 4. How We Protect Your Data

*   **Passwords:** Hashed with Argon2id. We cannot read them.
*   **Tokens:** Access tokens are short-lived. Refresh tokens are stored only as a hash. A stolen database cannot be replayed as a set of active sessions.
*   **Rate Limiting:** We protect against brute-force attacks using rate limits backed by Redis.
*   **Your Rights:** You can delete your account at any time. Your account is scheduled for deletion and you have a 30-day recovery window. After that window, your personal data is anonymized and your content is attributed to "Deleted User."

## 5. Your Data, Your Control

*   You can delete your account from Settings at any time.
*   You can recover it within 30 days by logging in.
*   You can see and revoke active sessions at any time.

## 6. Data Sharing

We do not sell your data. We use trusted service providers (like Resend for email delivery and Mapbox for maps) who process data on our behalf under strict agreements.

## 7. Contact Us

If you have questions about your data, contact us at [privacy@knot.app].
