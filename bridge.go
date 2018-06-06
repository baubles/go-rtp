package rtp

func Bridge(outputChan, inputChan chan interface{}) {
	go func() {
		for {
			data := <-outputChan
			inputChan <- data
		}
	}()
}
