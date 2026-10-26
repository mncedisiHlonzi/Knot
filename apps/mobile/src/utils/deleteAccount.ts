/**
 * Pure logic for the delete-account screen.
 *
 * Both rules live here rather than in the screen so they can be unit-tested without a
 * renderer, and so the screen reads as layout. The server is the authority: it
 * re-checks the password, and it refuses the deletion without it.
 */

/** The recovery window, mirroring `identity.DeletionRecoveryWindow` in the backend. */
export const RECOVERY_WINDOW_DAYS = 30;

/**
 * Returns the first thing preventing a deletion, or undefined when the form is
 * complete.
 *
 * The confirmation is checked before the password because it is the cheaper mistake
 * to make: a person who has not yet decided should be told what they are agreeing to,
 * not asked to type their password.
 */
export function validateDeleteAccountForm(
  password: string,
  confirmed: boolean,
): string | undefined {
  if (!confirmed) {
    return 'Tick the box to confirm you understand what happens next.';
  }
  if (password === '') {
    return 'Enter your password to confirm.';
  }

  return undefined;
}

/**
 * The consequences, in the order they matter to the person deciding.
 *
 * It is a list rather than a paragraph because a wall of text is exactly what people
 * skim past on a destructive screen.
 */
export const DELETION_CONSEQUENCES: readonly string[] = [
  `You will be signed out on every device, and your profile, stories and comments will be attributed to "Deleted User".`,
  `Your account is kept for ${RECOVERY_WINDOW_DAYS} days. Signing in during that time cancels the deletion and brings it back.`,
  `After ${RECOVERY_WINDOW_DAYS} days your email, name and contact details are erased for good. Your stories stay.`,
];
