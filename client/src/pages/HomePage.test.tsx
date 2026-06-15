import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

import { HomePage } from './HomePage'

describe('HomePage', () => {
    it('renders the primary call to action', () => {
        render(
            <MemoryRouter>
                <HomePage />
            </MemoryRouter>,
        )

        expect(screen.getByRole('link', { name: /create a room/i })).toBeInTheDocument()
        expect(screen.getByText(/guessing the model/i)).toBeInTheDocument()
    })
})
