// Organisation settings: the details printed on every invoice and receipt.
//
// These lived inside calendar.js, rendered at the bottom of a sub-tab called
// "Settings" underneath Schedule. Nothing about "schedule" suggests "the name,
// address and bank account on your invoices", and the dashboard checklist had
// to carry a pointer to it — a checklist item existing because a screen is
// unfindable is the tell.
//
// ADR-001: editing lives in context, creating vocabulary lives in setup. Class
// programmes and holidays are schedule vocabulary and stay there. Business
// details are setup, and live here, reachable from the dock like any other page.
(function() {
  'use strict';

  // _renderBusinessSettingsCard renders the form shell; values are filled in
  // asynchronously by _loadBusinessSettings once the GET resolves, so the
  // programs view can render synchronously like the rest of the module.
  function _renderBusinessSettingsCard() {
    return '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.75rem">'
      +   '<h2 style="font-size:1rem;font-weight:700;color:#111;margin:0">Business &amp; Invoice details <span style="font-size:0.72rem;font-weight:500;color:#94a3b8">(letterhead, bank &amp; receipt)</span></h2>'
      + '</div>'
      + '<div style="background:#fff;border-radius:0;border:1px solid rgba(0,0,0,0.07);box-shadow:0 1px 3px rgba(0,0,0,0.04);padding:1.5rem">'
      +   '<form id="business-settings-form" class="space-y-3" onsubmit="return App.Settings._saveBusinessSettings(event)">'
      +     _settingsField('Registered name', 'brandName', 'text', 'e.g. The Study Hub Sdn Bhd')
      +     _settingsField('Tagline', 'brandTagline', 'text', 'shown under the name')
      +     _settingsField('SSM / Business Reg. No', 'taxId', 'text', 'e.g. 201901012345 (1234567-A)')
      +     _settingsField('Address line 1', 'addressLine1', 'text', '')
      +     _settingsField('Address line 2', 'addressLine2', 'text', '')
      +     _settingsField('Support email', 'supportEmail', 'email', '')
      +     _settingsField('Support phone', 'supportPhone', 'text', '')
      +     _settingsField('Bank name', 'bankName', 'text', 'e.g. Maybank')
      +     _settingsField('Bank account no', 'bankAccountNo', 'text', '')
      +     _settingsField('Account holder name', 'bankAccountHolder', 'text', '')
      +     _settingsTextarea('Payment instructions', 'paymentInstructions', 'Shown on unpaid invoices')
      +     _settingsTextarea('Invoice terms', 'invoiceTerms', 'Terms & Conditions printed on the PDF')
      +     _settingsTextarea('Invoice footer notes', 'invoiceFooterHtml', 'Footer line on the PDF')
      +     '<div class="flex justify-end pt-2">'
      +       '<button type="submit" style="padding:0.5rem 1.1rem;font-size:0.85rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:4px;cursor:pointer">Save details</button>'
      +     '</div>'
      +   '</form>'
      + '</div>';
  }
  
  function _settingsField(label, name, type, placeholder) {
    return '<div><label class="block text-sm font-medium text-slate-700 mb-1">' + App.Utils.esc(label) + '</label>'
      + '<input name="' + name + '" type="' + type + '" class="form-input" placeholder="' + App.Utils.esc(placeholder) + '"></div>';
  }
  
  function _settingsTextarea(label, name, placeholder) {
    return '<div><label class="block text-sm font-medium text-slate-700 mb-1">' + App.Utils.esc(label) + '</label>'
      + '<textarea name="' + name + '" rows="2" class="form-input" placeholder="' + App.Utils.esc(placeholder) + '"></textarea></div>';
  }
  
  // _loadBusinessSettings fills the form with the current tenant settings.
  // Called after the programs view renders the form shell.
  function _loadBusinessSettings() {
    var form = document.getElementById('business-settings-form');
    if (!form) return;
    App.Api.get('/api/admin/settings').then(function(s) {
      if (!s) return;
      var fields = ['brandName','brandTagline','taxId','addressLine1','addressLine2',
        'supportEmail','supportPhone','bankName','bankAccountNo','bankAccountHolder',
        'paymentInstructions','invoiceTerms','invoiceFooterHtml'];
      fields.forEach(function(f) {
        if (form.elements[f]) form.elements[f].value = s[f] || '';
      });
    }).catch(function() {
      // Error already toasted by App.Api wrapper.
    });
  }
  
  function _saveBusinessSettings(e) {
    e.preventDefault();
    var fd = new FormData(e.target);
    var body = {
      brandName: fd.get('brandName').trim(),
      brandTagline: fd.get('brandTagline').trim(),
      taxId: fd.get('taxId').trim(),
      addressLine1: fd.get('addressLine1').trim(),
      addressLine2: fd.get('addressLine2').trim(),
      supportEmail: fd.get('supportEmail').trim(),
      supportPhone: fd.get('supportPhone').trim(),
      bankName: fd.get('bankName').trim(),
      bankAccountNo: fd.get('bankAccountNo').trim(),
      bankAccountHolder: fd.get('bankAccountHolder').trim(),
      paymentInstructions: fd.get('paymentInstructions').trim(),
      invoiceTerms: fd.get('invoiceTerms').trim(),
      invoiceFooterHtml: fd.get('invoiceFooterHtml').trim()
    };
    App.Api.put('/api/admin/settings', body).then(function() {
      App.Utils.showToast('Business details saved', 'success');
    }).catch(function() {
      // Error already toasted by App.Api wrapper.
    });
    return false;
  }
  

  async function render(container) {
    if (App.currentRole !== 'admin') {
      container.innerHTML = '<div style="padding:2rem;color:#94a3b8;font-size:0.9rem">Settings are admin only.</div>';
      return;
    }
    container.innerHTML =
        '<div style="max-width:100%;padding:0 0 2rem">'
      +   '<h1 style="font-family:Georgia,serif;font-size:1.5rem;font-weight:700;color:#111;margin:0 0 0.25rem">Settings</h1>'
      +   '<p style="font-size:0.86rem;color:#64748b;margin:0 0 1.5rem">What appears on every invoice and receipt you send.</p>'
      +   _renderBusinessSettingsCard()
      + '</div>';
    _loadBusinessSettings();
  }

  App.Settings = { render: render, _saveBusinessSettings: _saveBusinessSettings };
})();
