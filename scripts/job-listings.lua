token = os.getenv('LINKEDIN_TOKEN')
geoIdArgentina = '100446943'

request = {
  url = 'https://www.linkedin.com/voyager/api/voyagerJobsDashJobCards?decorationId=com.linkedin.voyager.dash.deco.jobs.search.JobSearchCardsCollection-220&q=jobSearch&query=(origin:JOB_SEARCH_PAGE_LOCATION_AUTOCOMPLETE,keywords:%22data%20scientist%22,locationUnion:(geoId:' .. geoIdArgentina .. '))&start=25&count=25',
  method = 'GET',
  headers = {
    ['Csrf-Token'] = 'csrf-token',
    ['Cookie'] = 'JSESSIONID="csrf-token"; li_at=' .. token,
    ['User-Agent'] = 'Mozilla/5.0 (X11; Linux x86_64; rv:142.0) Gecko/20100101 Firefox/142.0',
  },
}
