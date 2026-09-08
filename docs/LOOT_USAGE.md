# Loot Functionality

The C2 server has a fully functional loot management system for handling files downloaded from implanted machines.

## Overview

When you download files from compromised machines, they are automatically saved to the loot system with the following features:
- Files are stored in the `loot/` directory
- Metadata is tracked in the database (session, filename, UUID)
- Files can be listed, viewed, exported, and deleted

## Downloading Files from Implants

From a session, use the `download` command to retrieve files from the implanted machine:

```
(session - 12345)>> download /etc/passwd
```

When the implant responds, the file will be:
1. Saved to `loot/<UUID>` on disk
2. Registered in the database with session info and filename
3. A success message will be displayed with the UUID

## Loot Commands

Switch to loot mode from the main menu:

```
>> loot
(loot)>>
```

### List all loot files

```
(loot)>> list
```

This displays a table with:
- N: Entry number
- UUID: Unique identifier (shortened for display)
- SESSION: Session ID that downloaded the file
- FILENAME: Original filename from the target system

### View loot file contents

```
(loot)>> view <uuid>
```

Displays the content of the loot file directly in the terminal. Useful for quick inspection of text files.

### Export loot file

```
(loot)>> export <uuid> /path/to/save/file.txt
```

Copies the loot file to a specified location on the server's filesystem.

### Delete loot file

```
(loot)>> delete <uuid>
```

Removes both the physical file and database entry. This is permanent.

## UUID Matching

Commands accept partial UUIDs for convenience. The system will match the first loot entry containing the provided string:

```
(loot)>> export 46ce /tmp/exported_file.txt
```

This matches a UUID starting with or containing "46ce" only when the fragment
identifies exactly one entry. Ambiguous fragments are rejected; a full UUID
always resolves to its exact entry.

## File Storage

- **Physical files**: `loot/<UUID>` - Full UUID is used as filename. The
  directory is created automatically on the first download.
- **Database**: SQLite table with UUID, Session, and FileName columns
- **Permissions**: The directory is created with 0700 permissions and loot
  files with 0600 permissions.

## Implementation Details

### Download Flow

1. User issues `download <filename>` command in session
2. Task is created and sent to implant
3. Implant reads file and sends it back as CHUNK data
4. Server receives CHUNK, creates loot entry with:
   - Session ID
   - Original filename
   - File contents
   - Generated UUID
5. File is saved to `loot/<UUID>`
6. Database entry is created
7. Success message is logged with UUID

### Task Completion

When a file is downloaded:
- The task is marked as complete
- Response payload shows: "File downloaded: <filename> (<size> bytes) - UUID: <uuid>"
- Lua callbacks (if defined) are triggered
- Console shows success notification

## Error Handling

- If a session doesn't exist, chunk data is rejected
- If task ID is invalid, an error is logged
- File system errors during save are reported
- Database errors prevent file registration
- Database insertion failures remove the newly written file instead of leaving
  an orphan on disk
- Partial UUID matches that are missing or ambiguous will error
- Exporting replaces the destination content rather than appending to it

## Future Enhancements

Potential improvements:
- Add search/filter by session or filename
- Add file type detection and categorization
- Add compression for large files
- Add encryption at rest for sensitive loot
- Add bulk export functionality
- Add metadata like download timestamp and file size to display
