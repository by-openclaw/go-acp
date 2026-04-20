import { AllDestinationsAssociationNamesRequestMessageCommand } from '../../../../src/command/rx/102-all-destinations-association-names-request-message/command';
import { AllDestinationsAssociationNamesRequestMessageCommandParams } from '../../../../src/command/rx/102-all-destinations-association-names-request-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { AllDestinationsAssociationNamesRequestMessageCommandOptions } from '../../../../src/command/rx/102-all-destinations-association-names-request-message/options';
import { LengthOfNamesRequired } from '../../../../src/command/shared/length-of-names-required';

describe('All Destinations Association Names Request Message Command', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: AllDestinationsAssociationNamesRequestMessageCommandParams = {
                matrixId: 0
            };
            const options: AllDestinationsAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
            };
            // Act
            const command = new AllDestinationsAssociationNamesRequestMessageCommand(params, options);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: AllDestinationsAssociationNamesRequestMessageCommandParams = {
                matrixId: -1
            };

            const options: AllDestinationsAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: -1
            };
            // Act
            const fct = () => new AllDestinationsAssociationNamesRequestMessageCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: AllDestinationsAssociationNamesRequestMessageCommandParams = {
                matrixId: 256
            };

            const options: AllDestinationsAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: 10
            };
            // Act
            const fct = () => new AllDestinationsAssociationNamesRequestMessageCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General All Destinations Association Names Request Message CMD_102_0X66', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.ALL_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: AllDestinationsAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0
                };

                const options: AllDestinationsAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new AllDestinationsAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '66 00 00', // data
                    3, // bytesCount
                    0x97, // checksum
                    '10 02 66 00 00 03 97 10 03' // buffer
                );
            });
            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: AllDestinationsAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0
                };

                const options: AllDestinationsAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.EIGHT_CHAR_NAMES
                };
                // Act
                const command = new AllDestinationsAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '66 00 01', // data
                    3, // bytesCount
                    0x96, // checksum
                    '10 02 66 00 01 03 96 10 03' // buffer
                );
            });
            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: AllDestinationsAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0
                };

                const options: AllDestinationsAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.TWELVE_CHAR_NAMES
                };
                // Act
                const command = new AllDestinationsAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '66 00 02', // data
                    3, // bytesCount
                    0x95, // checksum
                    '10 02 66 00 02 03 95 10 03' // buffer
                );
            });
            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: AllDestinationsAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0
                };

                const options: AllDestinationsAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.SIXTEEN_CHAR_NAMES
                };
                // Act
                const command = new AllDestinationsAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '66 00 03', // data
                    3, // bytesCount
                    0x94, // checksum
                    '10 02 66 00 03 03 94 10 03' // buffer
                );
            });
        });

        describe('Extended General General All Source Names Request Message CMD_230_0Xe6', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.ALL_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE;
            });
            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: AllDestinationsAssociationNamesRequestMessageCommandParams = {
                    matrixId: 16
                };

                const options: AllDestinationsAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new AllDestinationsAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E6 10 00', // data
                    3, // bytesCount
                    0x07, // checksum
                    '10 02 E6 10 10 00 03 07 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: AllDestinationsAssociationNamesRequestMessageCommandParams = {
                    matrixId: 16
                };

                const options: AllDestinationsAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.EIGHT_CHAR_NAMES
                };
                // Act
                const command = new AllDestinationsAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E6 10 01', // data
                    3, // bytesCount
                    0x06, // checksum
                    '10 02 E6 10 10 01 03 06 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: AllDestinationsAssociationNamesRequestMessageCommandParams = {
                    matrixId: 16
                };

                const options: AllDestinationsAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.TWELVE_CHAR_NAMES
                };
                // Act
                const command = new AllDestinationsAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E6 10 02', // data
                    3, // bytesCount
                    0x05, // checksum
                    '10 02 E6 10 10 02 03 05 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: AllDestinationsAssociationNamesRequestMessageCommandParams = {
                    matrixId: 16
                };

                const options: AllDestinationsAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.SIXTEEN_CHAR_NAMES
                };
                // Act
                const command = new AllDestinationsAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E6 10 03', // data
                    3, // bytesCount
                    0x04, // checksum
                    '10 02 E6 10 10 03 03 04 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.ALL_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: AllDestinationsAssociationNamesRequestMessageCommandParams = {
                    matrixId: 16
                };

                const options: AllDestinationsAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.EIGHT_CHAR_NAMES
                };
                // Act
                const command = new AllDestinationsAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E6 10 01', // data
                    3, // bytesCount
                    0x06, // checksum
                    '10 02 E6 10 10 01 03 06 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general and extended command', () => {
        it('Should log general command description', () => {
            // Arrange
            const params: AllDestinationsAssociationNamesRequestMessageCommandParams = {
                matrixId: 0
            };

            const options: AllDestinationsAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
            };
            // Act
            const command = new AllDestinationsAssociationNamesRequestMessageCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });

        it('Should log extended command description', () => {
            // Arrange
            const params: AllDestinationsAssociationNamesRequestMessageCommandParams = {
                matrixId: 16
            };

            const options: AllDestinationsAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.EIGHT_CHAR_NAMES
            };
            // Act
            const command = new AllDestinationsAssociationNamesRequestMessageCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('extended')).toBe(true);
        });
    });
});
