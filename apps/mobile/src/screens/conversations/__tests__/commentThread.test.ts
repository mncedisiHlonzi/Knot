import { LANGUAGES } from '../../../data/languages';
import {
  DEFAULT_COMPOSE_LANGUAGE,
  MAX_CHIP_LENGTH,
  initialComposeLanguage,
  isReply,
  languageChipLabel,
  replyTargetId,
} from '../commentThread';

describe('replyTargetId', () => {
  it('targets the comment itself when it is top level', () => {
    expect(replyTargetId({ id: 'top', parent_comment_id: null })).toBe('top');
  });

  it('targets the top-level comment when replying to a reply', () => {
    // Threading is one level deep: a reply to a reply stays in the same thread,
    // which is what the server stores too (KNOT-ADR-047).
    expect(replyTargetId({ id: 'reply', parent_comment_id: 'top' })).toBe('top');
  });
});

describe('isReply', () => {
  it('reports a comment with no parent as top level', () => {
    expect(isReply({ parent_comment_id: null })).toBe(false);
  });

  it('reports a comment with a parent as a reply', () => {
    expect(isReply({ parent_comment_id: 'top' })).toBe(true);
  });
});

describe('languageChipLabel', () => {
  it('shows the name when it fits the chip', () => {
    expect(languageChipLabel('eng')).toBe('English');
    expect(languageChipLabel('zul')).toBe('Zulu');
  });

  it('keeps a name that is exactly the limit', () => {
    // "Abellen Ayta" is 12 characters, the widest name the chip still shows.
    expect('Abellen Ayta'.length).toBe(MAX_CHIP_LENGTH);
    expect(languageChipLabel('abp')).toBe('Abellen Ayta');
  });

  it('shows the code when the name is too long', () => {
    expect(languageChipLabel('tpx')).toBe('tpx');
  });

  it('falls back to an unknown code, because the server may know more than the app', () => {
    expect(languageChipLabel('xyz')).toBe('xyz');
  });

  it('has no label for an empty code, so the chip shows its placeholder', () => {
    expect(languageChipLabel('')).toBe('');
    expect(languageChipLabel('   ')).toBe('');
  });

  it('labels every canonical language within the chip width', () => {
    for (const language of LANGUAGES) {
      const label = languageChipLabel(language.code);
      expect(label.length).toBeLessThanOrEqual(MAX_CHIP_LENGTH);
      expect(label).toBe(language.name.length <= MAX_CHIP_LENGTH ? language.name : language.code);
    }
  });

  it('actually exercises the fallback, so the rule is not vacuous', () => {
    const tooLong = LANGUAGES.filter((language) => language.name.length > MAX_CHIP_LENGTH);
    expect(tooLong.length).toBeGreaterThan(0);
  });
});

describe('initialComposeLanguage', () => {
  it('uses the first preferred language when the app knows it', () => {
    expect(initialComposeLanguage('zul', 'eng')).toBe('zul');
  });

  it('falls back to the version language when the user has no preference', () => {
    expect(initialComposeLanguage(undefined, 'fra')).toBe('fra');
    expect(initialComposeLanguage('', 'fra')).toBe('fra');
  });

  it('ignores a preference the app does not know, e.g. a two-letter tag', () => {
    expect(initialComposeLanguage('zu', 'eng')).toBe('eng');
    expect(initialComposeLanguage('en')).toBe(DEFAULT_COMPOSE_LANGUAGE);
  });

  it('falls back to English when neither is usable', () => {
    expect(initialComposeLanguage(undefined, undefined)).toBe(DEFAULT_COMPOSE_LANGUAGE);
    expect(initialComposeLanguage('  ', '  ')).toBe(DEFAULT_COMPOSE_LANGUAGE);
  });
});
