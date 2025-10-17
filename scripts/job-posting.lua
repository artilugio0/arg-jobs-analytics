token = os.getenv('LINKEDIN_TOKEN')
postingId = '4277900147'

request = {
  url = 'https://www.linkedin.com/voyager/api/jobs/jobPostings/' .. postingId .. '?decorationId=com.linkedin.voyager.deco.jobs.web.shared.WebFullJobPosting-65&topN=1&topNRequestedFlavors=List(TOP_APPLICANT,IN_NETWORK,COMPANY_RECRUIT,SCHOOL_RECRUIT,HIDDEN_GEM,ACTIVELY_HIRING_COMPANY)',
  method = 'GET',
  headers = {
    ['Csrf-Token'] = 'csrf-token',
    ['Cookie'] = 'JSESSIONID="csrf-token"; li_at=' .. token,
    ['User-Agent'] = 'Mozilla/5.0 (X11; Linux x86_64; rv:142.0) Gecko/20100101 Firefox/142.0',
  },
}
