import { Link as RouterLink } from 'react-router-dom'
import { Button } from '@mui/material'
import ExploreOffOutlined from '@mui/icons-material/ExploreOffOutlined'
import EmptyState from '../components/EmptyState'

export default function NotFoundPage() {
  return (
    <EmptyState
      icon={ExploreOffOutlined}
      title="Page not found"
      description="The page you are looking for does not exist or you do not have access to it."
      action={
        <Button variant="contained" component={RouterLink} to="/">
          Back to dashboard
        </Button>
      }
    />
  )
}
