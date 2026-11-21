// A fake Logos folder with only a library catalogue (T22c): the columns the book list reads, and a
// few made-up books, some public domain, some not. No real Logos file is ever used.

import { DatabaseSync } from "node:sqlite";
import { mkdirSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

export function makeLogosLibraryFolder() {
  const root = mkdtempSync(join(tmpdir(), "glossa-logos-library-"));
  const data = join(root, "Data", "e2euser", "LibraryCatalog");
  mkdirSync(data, { recursive: true });
  const d = new DatabaseSync(join(data, "catalog.db"));
  d.exec(`CREATE TABLE Records (RecordId integer primary key autoincrement, ResourceId text not null, ResourceIdAsAlternate text,
    Type text, Title text, AbbreviatedTitle text, Authors text, PublicationDate text, Languages text, Copyright text,
    UserTitle text, UserAbbreviatedTitle text, Edition text);`);
  const add = d.prepare(`INSERT INTO Records (ResourceId, Type, Title, AbbreviatedTitle, Authors, PublicationDate, Languages, Copyright)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?)`);
  const books = [
    ["LLS:KJV", "text.monograph.bible", "King James Version", "KJV", "", "1900", "en", "Public Domain"],
    ["LLS:ESV", "text.monograph.bible", "English Standard Version", "ESV", "", "2016", "en", "Copyright 2016 Crossway"],
    ["LLS:EASTON", "text.monograph.dictionary", "Easton’s Bible Dictionary", "", "Easton, M. G.", "1893", "en", "Public domain"],
    ["LLS:BARNES", "text.monograph.commentary.bible", "Notes on the Gospels", "", "Barnes, Albert", "1847", "en", "Public Domain."],
    ["LLS:ROMANS", "text.monograph.commentary.bible", "A Commentary on Romans", "", "Moo, Douglas J.", "2018", "en", "Copyright 2018 Example Press"],
    ["LLS:GREEK", "text.monograph.lexicon", "A Greek Lexicon of Tests", "", "Alpha, Beta", "2001", "en", "Copyright 2001 Example Press"],
    ["LLS:SERMONS", "text.monograph", "Sermons of a Faithful Pastor", "", "Pastor, A.", "1850", "en", ""],
    ["LLS:VIDEO", "lbx.media", "A Lecture Series", "", "Teacher, T.", "2019", "en", "Copyright 2019 Example Media"],
  ];
  for (const b of books) add.run(...b);
  d.close();
  return root;
}
