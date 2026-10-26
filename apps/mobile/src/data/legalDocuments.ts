/**
 * The legal documents the app shows, bundled with the build.
 *
 * The canonical copies live in `docs/PRIVACY_POLICY.md` and
 * `docs/TERMS_OF_SERVICE.md`. They are copied here because the app has no way to
 * read a repository file at runtime and there is no endpoint that serves them: a
 * screen that fetched its own terms would need a session to read the text that
 * grants the session. `__tests__/legalDocuments.test.ts` reads the two markdown
 * files from disk and fails if this copy drifts from them (KNOT-035).
 *
 * The text is the founder-approved wording verbatim. Do not re-flow or re-word it
 * here; edit the markdown files and re-copy.
 */

/** Which document a screen was asked to show. */
export type LegalDocument = 'privacy' | 'terms';

/**
 * The Privacy Policy, exactly as `docs/PRIVACY_POLICY.md` holds it.
 *
 * A template literal rather than a concatenation so the paragraph breaks stay
 * visible in review.
 */
export const PRIVACY_POLICY = `# Knot — Privacy Policy

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
`;

/** The Terms of Service, exactly as `docs/TERMS_OF_SERVICE.md` holds it. */
export const TERMS_OF_SERVICE = `# Knot — Terms of Service

**Last Updated: [Date to be set at launch]**

Welcome to Knot. Knot is a platform where people share and adapt stories across languages and cultures. These Terms explain the rules of the road. By using Knot, you agree to them.

## 1. Who Can Use Knot

You must be at least 13 years old to use Knot. You are responsible for keeping your account secure.

## 2. What You Can Share

You retain ownership of the content you create. By posting on Knot, you grant Knot a license to display and distribute it on the platform. You agree not to post content that:
*   Is illegal or promotes illegal acts.
*   Is hateful, harassing, or threatening.
*   Infringes on someone else's intellectual property.
*   Is misleading or designed to deceive.

We do not use AI to automatically moderate content. Moderation is done by humans and the community.

## 3. Adaptations (Tell My People)

Knot allows users to adapt stories for other languages and communities. When you adapt a story, you acknowledge that the original author retains ownership of the original story, and your adaptation is linked to it in the Language Tree. Your adaptation is attributed to you.

## 4. Respectful Disagreement

Knot is built on the idea that people can disagree respectfully. The "Rings true," "I know it differently," "Adds something new," and "Needs a source" reactions are not likes or votes. They are perspective signals. They are not used to rank or suppress stories.

## 5. Safety and Moderation

*   You can report content that violates these Terms.
*   You can block other users.
*   Moderators may remove content that violates these Terms.
*   Serious or repeated violations may result in account suspension or deletion.

## 6. Account Deletion

You can delete your account at any time. Your account is deactivated and scheduled for deletion with a 30-day recovery window. After the window, your personal data is anonymized and your content is attributed to "Deleted User."

## 7. Changes to These Terms

We may update these Terms. If we make material changes, we will notify you. Continued use of Knot after changes means you accept the new Terms.

## 8. Disclaimer

Knot is provided "as is." We do not guarantee that it will be error-free or uninterrupted. We are not responsible for content posted by users.

## 9. Contact

If you have questions about these Terms, contact us at [legal@knot.app].
`;

/**
 * The text of one document.
 *
 * It is a function rather than an object keyed by the type so a new document
 * cannot be added to one place and forgotten in the other: TypeScript demands the
 * exhaustive branch below.
 */
export function legalDocumentText(document: LegalDocument): string {
  return document === 'privacy' ? PRIVACY_POLICY : TERMS_OF_SERVICE;
}

/** The document's name, as the screen header and the register form's links say it. */
export function legalDocumentTitle(document: LegalDocument): string {
  return document === 'privacy' ? 'Privacy Policy' : 'Terms of Service';
}
