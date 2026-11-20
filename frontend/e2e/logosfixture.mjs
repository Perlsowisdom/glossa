// A small fake Logos data folder the tests build themselves (T22a): the same tables the importer
// reads, with a little of everything. No real Logos file or note is ever used.

import { DatabaseSync } from "node:sqlite";
import { mkdirSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";

const NOTES = `
CREATE TABLE Notes (NoteId integer primary key autoincrement, ExternalId text not null, CreatedDate text not null,
  ModifiedDate text, IsDeleted bool not null default 0, IsTrashed bool not null default 0, Kind int not null,
  ContentRichText text, FoldedContent text, NoteColorId int, NoteStyleId int, AnchorsJson text, TagsJson text,
  NotebookExternalId text not null, Role int not null default 1);
CREATE TABLE Notebooks (NotebookId integer primary key autoincrement, ExternalId text not null, Title text,
  IsDeleted bool not null default 0, IsTrashed bool not null default 0, Role int not null default 1);
CREATE TABLE NoteStyles (NoteStyleId integer primary key autoincrement, Name text not null);
CREATE TABLE NoteColors (NoteColorId integer primary key autoincrement, Name text not null);
CREATE TABLE NoteAnchorFacetReferences (NoteAnchorFacetReferenceId integer primary key autoincrement, NoteId integer not null,
  AnchorIndex int not null, DataTypeId int not null, BibleBook int, SortKey blob, Reference text not null);`;

const run = (text, attrs = "") => `<Paragraph><Run ${attrs} Text="${text}"/></Paragraph>`;

// pack writes text the way Logos packs it: a marker byte (2: as it is), a length, the text.
function pack(text) {
  const body = Buffer.from(text, "utf8");
  const head = Buffer.alloc(5);
  head[0] = 2;
  head.writeUInt32LE(body.length, 1);
  return Buffer.concat([head, body]);
}

function db(dir, rel) {
  const path = join(dir, ...rel.split("/"));
  mkdirSync(dirname(path), { recursive: true });
  return new DatabaseSync(path);
}

// makeLogosFolder builds <tmp>/glossa-logos-fixture-*/{Data,Documents}/e2euser and returns the root.
export function makeLogosFolder() {
  const root = mkdtempSync(join(tmpdir(), "glossa-logos-fixture-"));
  const docs = join(root, "Documents", "e2euser");
  const data = join(root, "Data", "e2euser");

  let d = db(data, "LibraryCatalog/catalog.db");
  d.exec(`CREATE TABLE Records (ResourceId text not null, ResourceIdAsAlternate text, Title text not null, UserTitle text, Type text not null);
    INSERT INTO Records VALUES ('LLS:ESV', NULL, 'English Standard Version', NULL, 'text.monograph.bible'),
      ('LLS:COMM', NULL, 'Fixture Commentary on Romans', NULL, 'text.monograph.commentary.bible');`);
  d.close();

  d = db(docs, "NotesToolManager/notestool.db");
  d.exec(NOTES);
  d.exec(`INSERT INTO NoteStyles VALUES (1, 'custom:YellowHighlighter'), (2, 'custom:RedUnderline'), (3, 'none');
    INSERT INTO Notebooks (ExternalId, Title) VALUES ('nb1', 'Romans study');
    INSERT INTO Notebooks (ExternalId, Title, Role) VALUES ('nb2', 'Shared guide', 3);`);
  const note = d.prepare(`INSERT INTO Notes (ExternalId, CreatedDate, ModifiedDate, Kind, ContentRichText, AnchorsJson, TagsJson, NotebookExternalId, NoteStyleId, Role)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`);
  note.run("n1", "2020-01-22T10:57:51Z", "2020-01-22T10:58:11Z", 0,
    `<Paragraph><Run FontBold="True" Text="Grace"/><Run Text=" alone, see "/><UriLink Uri="https://example.org/x"><Run Text="this page"/></UriLink></Paragraph>`,
    '[{"reference":{"raw":"bible+esv.66.8.28-66.8.30"}}]', '[{"plain":{"text":"Grace"}}]', "nb1", 3, 1);
  note.run("n2", "2021-03-01T00:00:00Z", null, 0, run("Check the Greek here"),
    '[{"textRange":{"resourceId":"LLS:COMM","version":"1","offset":10,"length":5}}]', null, "nb1", 3, 1);
  note.run("n3", "2021-03-02T00:00:00Z", null, 0, run("Someone else wrote this"), null, null, "nb2", 3, 3);
  note.run("n4", "2021-03-03T00:00:00Z", null, 0, "", null, null, "nb1", 3, 1);
  note.run("h1", "2019-12-17T01:21:25Z", null, 1, null, '[{"reference":{"raw":"bible+esv.64.3.16"}}]', null, "nb1", 1, 1);
  note.run("h2", "2019-12-18T01:21:25Z", null, 1, null, '[{"textRange":{"resourceId":"LLS:COMM","version":"1","offset":1,"length":2}}]', null, "nb1", 2, 1);
  note.run("h3", "2019-12-19T01:21:25Z", null, 1, null, '[{"textRange":{"resourceId":"LLS:ESV","version":"1","offset":1,"length":2}}]', null, "nb1", 2, 1);
  d.exec(`INSERT INTO NoteAnchorFacetReferences (NoteId, AnchorIndex, DataTypeId, Reference) VALUES (7, 0, 1, 'bible+esv.66.5.1'), (7, 0, 1, 'bible+esv.66.5.2')`);
  d.close();

  d = db(docs, "Documents/Clippings/Clippings.db");
  d.exec(`CREATE TABLE ClippingsDocuments (RowId integer primary key autoincrement, DocumentId blob not null, IsDeleted bool not null default 0, ModifiedDate text, Title text not null);
    CREATE TABLE Clippings (RowId integer primary key autoincrement, ItemId blob not null, Revision blob not null, DocumentRowId integer not null,
      ResourceId text not null, CreatedDate text not null, Title blob not null, Content blob not null, Notes blob, Tags text, Rank integer);
    INSERT INTO ClippingsDocuments (DocumentId, Title) VALUES (x'0102', 'Word studies');`);
  d.prepare(`INSERT INTO Clippings (ItemId, Revision, DocumentRowId, ResourceId, CreatedDate, Title, Content, Notes, Tags, Rank)
    VALUES (x'01', x'0101', 1, 'LLS:COMM', '2020-04-29T13:27:13-07:00', ?, ?, ?, 'greek', 1)`)
    .run(pack(run("agape")), pack(run("the love of God", 'FontItalic="True"')), pack(""));
  d.close();

  d = db(docs, "Documents/PassageList/PassageList.db");
  d.exec(`CREATE TABLE PassageLists (Id integer primary key autoincrement, DocumentGuid blob not null, Title text,
    CompressedItems blob not null, ModifiedDate text, IsDeleted bool not null default 0);`);
  d.prepare(`INSERT INTO PassageLists (DocumentGuid, Title, CompressedItems, ModifiedDate) VALUES (x'01020304050607080910111213141516', 'Memory verses', ?, '2024-07-21T16:43:08-04:00')`)
    .run(pack('[{"ref":"bible+nrsv.64.3.16"},{"ref":"bible.66.8.28-66.8.30"}]'));
  d.close();

  d = db(docs, "Documents/ReadingPlan/ReadingPlan.db");
  d.exec(`CREATE TABLE ReadingPlanDocuments (Id integer primary key autoincrement, DocumentId text not null, Title text, Sessions text,
    ModifiedDate text, ScheduleId blob, IsDeleted int not null);
    CREATE TABLE ReadingPlanStatus (Id integer primary key autoincrement, SyncId text not null, ReadSessions text, IsDeleted int not null);
    INSERT INTO ReadingPlanStatus (SyncId, ReadSessions, IsDeleted) VALUES ('24a873f1e93f4ad2b337934906d5174a', '["2024-02-14"]', 0);`);
  d.prepare(`INSERT INTO ReadingPlanDocuments (DocumentId, Title, Sessions, ModifiedDate, ScheduleId, IsDeleted)
    VALUES ('Document:ReadingPlan:aaaa', 'John in three days', ?, '2024-02-16T11:22:49-05:00', x'F173A8243FE9D24AB337934906D5174A', 0)`)
    .run(JSON.stringify([
      { date: "2024-02-14", readings: [[{ resourceId: "LLS:ESV", reference: "bible+nrsv.64.1" }]] },
      { date: "2024-02-15", readings: [[{ resourceId: "LLS:ESV", reference: "bible+nrsv.64.2" }]] },
      { date: "2024-02-16", readings: [[{ resourceId: "LLS:ESV", reference: "bible+nrsv.64.3" }]] },
    ]));
  d.close();
  return root;
}
