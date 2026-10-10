function filterDocs() {
  const query = document.getElementById('docs-search').value.toLowerCase().trim();
  const links = document.querySelectorAll('.docs-nav-link');
  const sections = document.querySelectorAll('.docs-nav-section');

  if (!query) {
    links.forEach(l => l.style.display = '');
    sections.forEach(s => s.style.display = '');
    return;
  }

  sections.forEach(section => {
    const sectionLinks = section.querySelectorAll('.docs-nav-link');
    let hasVisible = false;

    sectionLinks.forEach(link => {
      const text = link.textContent.toLowerCase();
      if (text.includes(query)) {
        link.style.display = '';
        hasVisible = true;
      } else {
        link.style.display = 'none';
      }
    });
    section.style.display = hasVisible ? '' : 'none';
  });
}

document.addEventListener('DOMContentLoaded', () => {
  const sections = document.querySelectorAll('.docs-article section');
  const links = document.querySelectorAll('.docs-nav-link');

  if (!sections.length) return;

  const observer = new IntersectionObserver((entries) => {
    entries.forEach(entry => {
      if (entry.isIntersecting) {
        const id = entry.target.id;
        links.forEach(link => {
          link.classList.toggle('active', link.getAttribute('href') === '#' + id);
        });
      }
    });
  }, { rootMargin: '-30% 0px -60% 0px' });

  sections.forEach(section => observer.observe(section));
});