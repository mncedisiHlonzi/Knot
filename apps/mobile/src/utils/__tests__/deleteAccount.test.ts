import {
  DELETION_CONSEQUENCES,
  RECOVERY_WINDOW_DAYS,
  validateDeleteAccountForm,
} from '../deleteAccount';

/**
 * The delete-account form's rules.
 *
 * They mirror what the screen enforces before it calls the API: the confirmation comes
 * first because it is the cheaper mistake, and the password is required because the
 * server will refuse the request without it anyway.
 */
describe('validateDeleteAccountForm', () => {
  it('accepts a confirmed form with a password', () => {
    expect(validateDeleteAccountForm('correct horse 7', true)).toBeUndefined();
  });

  it('requires the confirmation before anything else', () => {
    expect(validateDeleteAccountForm('', false)).toBe(
      'Tick the box to confirm you understand what happens next.',
    );
    expect(validateDeleteAccountForm('correct horse 7', false)).toBe(
      'Tick the box to confirm you understand what happens next.',
    );
  });

  it('requires a password once the box is ticked', () => {
    expect(validateDeleteAccountForm('', true)).toBe('Enter your password to confirm.');
  });
});

describe('DELETION_CONSEQUENCES', () => {
  it('states the recovery window, because that is what makes it safe', () => {
    expect(DELETION_CONSEQUENCES.join(' ')).toContain(`${RECOVERY_WINDOW_DAYS} days`);
  });

  it('says the content stays and the person goes', () => {
    const copy = DELETION_CONSEQUENCES.join(' ');

    expect(copy).toContain('Deleted User');
    expect(copy).toContain('Your stories stay');
  });
});
