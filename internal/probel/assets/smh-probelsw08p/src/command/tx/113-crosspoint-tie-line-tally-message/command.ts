import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandItemsUtility } from './items';
import { CommandParamsUtility, CrossPointTieLineTallyCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the CrossPoint Tie Line Tally Message command
 *
 * This message is issued by the controller returning association based tally information in response to a CROSSPOINT TIE LINE INTERROGATE message (Command Byte 112).
 * The command returns a tally for every level where a source is connected to the destination association.
 *
 * Command issued by Pro-Bel Controller
 *
 * @export
 * @class CrossPointTieLineTallyCommand
 * @extends {CommandBase<CrossPointTieLineTallyCommandParams>}
 */
export class CrossPointTieLineTallyCommand extends CommandBase<CrossPointTieLineTallyCommandParams, any> {
    /**
     * Creates an instance of CrossPointTieLineTallyCommand.
     * @param {CrossPointTieLineTallyCommandParams} params the command parameters
     * @memberof CrossPointTieLineTallyCommand
     */
    constructor(params: CrossPointTieLineTallyCommandParams) {
        super(CommandIdentifiers.TX.GENERAL.CROSSPOINT_TIE_LINE_TALLY_MESSAGE, params);
        this.validateParams(params);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof CrossPointTallyDumpByteCommand
     */
    toLogDescription(): string {
        return `General  -   ${this.name}: ${CommandParamsUtility.toString(
            this.params
        )}, ${CommandItemsUtility.toString(this.params.sourceItems[0])}`;
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof CrossPointTieLineTallyCommand
     */
    protected buildData(): Buffer {
        return this.buildDataNormal();
    }

    /**
     * Builds the general command
     * Returns Probel SW-P-08 - General CrossPoint Tie Line Tally Message CMD_113_0X71
     *
     * + This message is issued by the controller returning association based tally information in response to a CROSSPOINT TIE LINE INTERROGATE message (Command Byte 112).
     * + The command returns a tally for every level where a source is connected to the destination association.
     *
     * | Message                        | Command Byte  | 113-0x71                                                                                                     |
     * |--------------------------------|---------------|--------------------------------------------------------------------------------------------------------------|
     * | Byte                           | Field Format  | Notes                                                                                                        |
     * | Byte 1                         | Dest Matrix   | Destination Matrix Number (0-19)                                                                             |
     * | Byte 2                         | Dest Ass mult | Destination Association Number DIV 256                                                                       |
     * | Byte 3                         | Dest Ass num  | Destination Association Number MOD 256                                                                       |
     * | Byte 4                         | Num Srcs      | Number of Sources returned                                                                                   |
     * | Byte 5                         | Src 0 Matrix  | Source 0 Matrix                                                                                              |
     * | Byte 6                         | Src 0 Lev     | Source 0 Level                                                                                               |
     * | Byte 7                         | Src 0 mult    | Source 0 DIV 256                                                                                             |
     * | Byte 8                         | Src 0 num     | Source 0 MOD 256                                                                                             |
     * | Byte (n*4)+5                   | Src n Matrix  | Source n Matrix                                                                                              |
     * | Byte (n*4)+6                   | Src n Lev     | Source n Level                                                                                               |
     * | Byte (n*4)+7                   | Src n mult    | Source n DIV 256                                                                                             |
     * | Byte (n*4)+8                   | Src n num     | Source n MOD 256                                                                                             |
     *
     * @private
     * @returns {Buffer}
     * @memberof CrossPointTieLineTallyCommand
     */
    private buildDataNormal(): Buffer {
        const buffer = new SmartBuffer({ size: 9 + (this.params.numberOfSourcesReturned - 1) * 4 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.destinationMatrixId)
            .writeUInt8(Math.floor(this.params.destinationAssociation / 256))
            .writeUInt8(this.params.destinationAssociation % 256)
            .writeUInt8(this.params.numberOfSourcesReturned);

        for (let byteIndex = 0; byteIndex < this.params.numberOfSourcesReturned; byteIndex++) {
            const data = this.params.sourceItems[byteIndex];
            buffer
                .writeUInt8(data.sourceMatrixId)
                .writeUInt8(data.sourceLevel)
                .writeUInt8(Math.floor(data.sourceId / 256))
                .writeUInt8(data.sourceId % 256);
        }
        return buffer.toBuffer();
    }

    /**
     * Validate the parameters, options and items and throw a ValidationError in case of error
     * @private
     * @param {CrossPointTieLineTallyCommandParams} params command parameters
     * @param {DestinationAssociationNamesResponseCommandOptions} options the command items
     * @memberof CrossPointTieLineTallyCommand
     */
    private validateParams(params: CrossPointTieLineTallyCommandParams): void {
        const validationErrors: Record<string, LocaleData> = {
            ...new CommandParamsValidator(params).validate()
        };

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }
}
