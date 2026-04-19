// Skooly Student Scraper
// ======================
// Paste this entire script into the browser console (F12 → Console)
// while on the Skooly Students page (show ALL students, not filtered).
//
// It reads every row on the current page, then auto-clicks "Next" to
// collect all pages. When done, it copies the JSON to your clipboard.
//
// Usage:
//   1. Go to Skooly → Students → set view to show ALL students
//   2. Open dev console (F12 → Console)
//   3. Paste this script and press Enter
//   4. Wait for "Done! X students copied to clipboard"
//   5. Paste into a file: scripts/skooly_students.json

(async function scrapeSkooly() {
  const allStudents = [];
  let pageNum = 0;

  function scrapeCurrentPage() {
    const rows = document.querySelectorAll('table tbody tr, .student-list-item, [class*="student"] tr');
    if (rows.length === 0) {
      // Try alternative selectors — Skooly's DOM varies by version
      const altRows = document.querySelectorAll('[class*="row"], [class*="Row"]');
      if (altRows.length > 0) return scrapeRows(altRows);
      console.warn('No rows found. Try: copy the page HTML and send it to Claude.');
      return [];
    }
    return scrapeRows(rows);
  }

  function scrapeRows(rows) {
    const students = [];
    rows.forEach(function(row) {
      const cells = row.querySelectorAll('td, [class*="cell"], [class*="col"]');
      if (cells.length < 4) return; // skip header or empty rows

      // Extract text from each cell, trimming whitespace
      const texts = Array.from(cells).map(function(c) { return c.innerText.trim(); });

      // Try to find the student name (usually first meaningful cell)
      // Skooly format from screenshots:
      // [checkbox] | Student name + ID + Siblings | Branch | Batch | DOB | Registered On | Contact | Status | Action

      const nameCell = cells[1] || cells[0];
      const nameText = nameCell ? nameCell.innerText.trim() : '';
      const lines = nameText.split('\n').map(function(l) { return l.trim(); }).filter(Boolean);

      let studentName = lines[0] || '';
      let skolyId = '';
      let siblings = '';

      lines.forEach(function(line) {
        if (line.startsWith('ID:')) skolyId = line.replace('ID:', '').trim();
        if (line.startsWith('Siblings')) siblings = line.replace('Siblings', '').trim();
      });

      // Contact cell — has parent name, tag (Mom/Father), email, phone
      const contactCell = cells[6] || cells[5];
      let parentName = '', parentEmail = '', parentPhone = '', parentRole = '';
      if (contactCell) {
        const contactText = contactCell.innerText.trim();
        const contactLines = contactText.split('\n').map(function(l) { return l.trim(); }).filter(Boolean);
        contactLines.forEach(function(line) {
          if (line.includes('@')) parentEmail = line;
          else if (/^6\d{8,}$/.test(line.replace(/\D/g,''))) parentPhone = line;
          else if (line === 'Mom' || line === 'Father' || line === 'Mother') parentRole = line;
          else if (!parentName && line.length > 1) parentName = line;
        });
      }

      // Status cell
      const statusCell = cells[7] || cells[6];
      let status = 'Active';
      if (statusCell) {
        const st = statusCell.innerText.trim();
        if (st.includes('Inactive')) status = 'Inactive';
        else if (st.includes('New')) status = 'New';
        else if (st.includes('Active')) status = 'Active';
      }

      // Other cells
      const branch = cells[2] ? cells[2].innerText.trim() : '';
      const batch = cells[3] ? cells[3].innerText.trim() : '';
      const dob = cells[4] ? cells[4].innerText.trim() : '';
      const registeredOn = cells[5] ? cells[5].innerText.trim() : '';

      if (studentName && studentName !== 'Student name') {
        students.push({
          name: studentName,
          skolyId: skolyId,
          branch: branch,
          batch: batch,
          dob: dob,
          registeredOn: registeredOn,
          status: status,
          parentName: parentName,
          parentEmail: parentEmail,
          parentPhone: parentPhone,
          parentRole: parentRole,
          siblings: siblings
        });
      }
    });
    return students;
  }

  // Scrape current page
  const currentPageStudents = scrapeCurrentPage();
  allStudents.push(...currentPageStudents);
  pageNum++;
  console.log('Page ' + pageNum + ': found ' + currentPageStudents.length + ' students');

  // Try to auto-paginate
  async function goNextPage() {
    const nextBtn = document.querySelector('a:not(.disabled)[aria-label="Next"], button:not(.disabled):not([disabled]) span, a:not(.disabled)');
    // More specific: look for pagination links
    const links = document.querySelectorAll('a, button');
    for (const link of links) {
      const text = link.innerText.trim().toLowerCase();
      if ((text === 'next' || text === '>' || text === '>>') && !link.classList.contains('disabled') && !link.disabled) {
        link.click();
        await new Promise(function(r) { setTimeout(r, 2000); }); // wait for page load
        return true;
      }
    }
    return false;
  }

  // Auto-paginate through all pages
  let hasNext = true;
  while (hasNext) {
    hasNext = await goNextPage();
    if (hasNext) {
      const pageStudents = scrapeCurrentPage();
      if (pageStudents.length === 0) break; // safety: empty page = done
      // Dedup by name (in case pagination overlap)
      const existingNames = new Set(allStudents.map(function(s) { return s.name; }));
      const newStudents = pageStudents.filter(function(s) { return !existingNames.has(s.name); });
      if (newStudents.length === 0) break; // no new students = we've looped
      allStudents.push(...newStudents);
      pageNum++;
      console.log('Page ' + pageNum + ': found ' + newStudents.length + ' new students');
    }
  }

  // Copy to clipboard
  const json = JSON.stringify(allStudents, null, 2);
  try {
    await navigator.clipboard.writeText(json);
    console.log('Done! ' + allStudents.length + ' students copied to clipboard. Paste into scripts/skooly_students.json');
  } catch(e) {
    // Clipboard API might be blocked — fallback
    copy(json); // Chrome console helper
    console.log('Done! ' + allStudents.length + ' students. JSON copied via copy().');
  }
  console.log('Preview:', allStudents.slice(0, 3));
})();
