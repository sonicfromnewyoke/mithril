package replay

import (
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/global"
	"github.com/sonicfromnewyoke/mithril/pkg/sealevel"
	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/require"
)

func TestRebuildAuthorizedVotersFromVoteCacheIncludesV4(t *testing.T) {
	epoch := uint64(973)
	voteAcct := solana.PublicKey{0x44}
	authorizedVoter := solana.PublicKey{0x55}

	oldCache := global.EpochAuthorizedVoters()
	defer global.SetEpochAuthorizedVoters(oldCache)
	defer global.DeleteVoteCacheItem(voteAcct)

	var authorizedVoters sealevel.AuthorizedVoters
	authorizedVoters.AuthorizedVoters.Set(epoch, authorizedVoter)

	global.PutVoteCacheItem(voteAcct, &sealevel.VoteStateVersions{
		Type: sealevel.VoteStateVersionV4,
		V4: sealevel.VoteState4{
			AuthorizedVoters: authorizedVoters,
		},
	})

	rebuildAuthorizedVotersFromVoteCache(epoch)

	cache := global.EpochAuthorizedVoters()
	require.NotNil(t, cache)
	require.True(t, cache.IsAuthorizedVoter(voteAcct, authorizedVoter))
}
