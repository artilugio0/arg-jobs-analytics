token = os.getenv('LINKEDIN_TOKEN')

request = {
  url = 'https://www.linkedin.com/voyager/api/voyagerJobsDashJobCards?decorationId=com.linkedin.voyager.dash.deco.jobs.search.JobSearchCardsCollectionLite-88&q=jobSearch&query=(origin:SWITCH_SEARCH_VERTICAL,keywords:%22data%20scientist%22)&start=0&count=100',
  method = 'GET',
  headers = {
    ['Csrf-Token'] = 'csrf-token',
    ['Cookie'] = 'JSESSIONID="csrf-token"; li_at=' .. token,
    ['User-Agent'] = 'Mozilla/5.0 (X11; Linux x86_64; rv:142.0) Gecko/20100101 Firefox/142.0',
  },
}
