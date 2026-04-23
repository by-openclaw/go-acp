import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandParamsUtility, CrossPointTieLineInterrogateMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the CrossPoint Tie Line Ineterrogate Message command
 *
 * This message is issued by the remote device to request tallies for a given matrix and destination association number.

 * Command issued by the remote device
 * @export
 * @class CrossPointTieLineIneterrogateMessageCommand
 * @extends {CommandBase<CrossPointTieLineInterrogateMessageCommandParams>}
 */
export class CrossPointTieLineIneterrogateMessageCommand extends CommandBase<
    CrossPointTieLineInterrogateMessageCommandParams,
    any
> {
    /**
     * Creates an instance of CrossPointTieLineIneterrogateMessageCommand
     *
     * @param {CrossPointTieLineInterrogateMessageCommandParams} params the command parameters
     * @memberof CrossPointTieLineIneterrogateMessageCommand
     */
    constructor(params: CrossPointTieLineInterrogateMessageCommandParams) {
        super(CommandIdentifiers.RX.GENERAL.CROSSPOINT_TIE_LINE_INTERROGATE_MESSAGE, params);
        this.validateParams(params);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof CrossPointTieLineIneterrogateMessageCommand
     */
    toLogDescription(): string {
        return `General -${CommandParamsUtility.toString(this.params)} `;
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof CrossPointTieLineIneterrogateMessageCommand
     */
    protected buildData(): Buffer {
        return this.buildDataNormal();
    }

    /**
     * Build the command
     * Returns Probel SW-P-08 - General CrossPoint Tie-Line Ineterrogate Message CMD_112_0X70
     *
     * + This message is issued by the remote device to request tallies for a given matrix and destination association number.
     * + The controller responds with a CROSSPOINT TIE LINE TALLY message (Command Byte 113)
     *
     * | Message | Command Byte | 112 - 0x70                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  | Matrix number| Matrix group number                                                                                                                |
     * | Byte 2  | Dest multiplier| Destination number DIV 256                                                                                                       |
     * | Byte 3  | Dest number  | Destination number MOD 256                                                                                                         |
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof CrossPointTieLineInterrogateMessageCommandParams
     */
    protected buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 4 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.matrixId)
            .writeUInt8(Math.floor(this.params.destinationId / 256))
            .writeUInt8(this.params.destinationId % 256)
            .toBuffer();
    }

    /**
     * Validates the command parameter(w) and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {CrossPointTieLineInterrogateMessageCommandParams} params the command parameters
     * @memberof CrossPointTieLineIneterrogateMessageCommand
     */
    private validateParams(params: CrossPointTieLineInterrogateMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = new CommandParamsValidator(params).validate();

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }
}
